package desktop

import (
	"math"
	"sort"

	"opensync/internal/autosync"
)

type auditionSpan struct {
	start, end float64
	gain       float32
}

type auditionClip struct {
	index int
	spans []auditionSpan
}

// Plan on the timeline rather than the playback clock, so forward and reverse
// playback use identical transitions. Only the audition mix changes.
func planAudition(p *Project, tracks []int, start, end float64, fallback bool) []auditionClip {
	type source struct {
		index      int
		track      int
		start, end float64
	}
	var candidates []source
	selected := make(map[int]bool, len(tracks))
	boundaries := []float64{start, end}
	add := func(track int) {
		clips := p.Tracks[track].Clips
		first := sort.Search(len(clips), func(i int) bool { return p.Clips[clips[i]].End > start })
		for _, idx := range clips[first:] {
			c := p.Clips[idx]
			if c.Start >= end {
				break
			}
			if !c.Audio || (fallback && c.Missing) {
				continue
			}
			lo, hi := math.Max(start, c.Start), math.Min(end, c.End)
			if fallback && len(c.Mapping) >= 2 {
				if c.AudioOrigin > 0 {
					lo = math.Max(lo, autosync.MapTime(c.Mapping, c.AudioOrigin))
				}
				if duration := metadataFloat(c.Metadata.Audio["duration"]); duration > 0 {
					hi = math.Min(hi, autosync.MapTime(c.Mapping, c.AudioOrigin+duration))
				}
			}
			if lo >= hi {
				continue
			}
			candidates = append(candidates, source{idx, track, lo, hi})
			if fallback {
				boundaries = append(boundaries, lo, hi)
			}
		}
	}
	for _, track := range tracks {
		selected[track] = true
		add(track)
	}
	if !fallback {
		mix := make([]auditionClip, 0, len(candidates))
		for _, c := range candidates {
			mix = append(mix, auditionClip{c.index, []auditionSpan{{c.start, c.end, 1 / float32(len(tracks))}}})
		}
		return mix
	}
	// Prefer dedicated audio lanes, then use the other lanes in timeline order.
	for _, kind := range []string{"audio", "video"} {
		for i, t := range p.Tracks {
			if !selected[i] && (t.Kind == "audio") == (kind == "audio") {
				add(i)
			}
		}
	}
	sort.Float64s(boundaries)
	var mix []auditionClip
	byIndex := map[int]int{}
	for i := 1; i < len(boundaries); i++ {
		lo, hi := boundaries[i-1], boundaries[i]
		if hi <= lo {
			continue
		}
		var primary, alternate [2]source
		n, m := 0, 0
		for _, c := range candidates {
			if c.start > lo || c.end < hi {
				continue
			}
			if selected[c.track] && n < 2 {
				primary[n], n = c, n+1
			} else if !selected[c.track] && m < 2 {
				alternate[m], m = c, m+1
			}
		}
		active := primary[:n]
		gain := 1 / float32(len(tracks))
		if n == 0 && m > 0 {
			active, gain = alternate[:m], 1/float32(m)
		}
		for _, c := range active {
			item, exists := byIndex[c.index]
			if !exists {
				item = len(mix)
				byIndex[c.index] = item
				mix = append(mix, auditionClip{index: c.index})
			}
			spans := mix[item].spans
			if last := len(spans) - 1; last >= 0 && spans[last].end == lo && spans[last].gain == gain {
				spans[last].end = hi
			} else {
				mix[item].spans = append(spans, auditionSpan{lo, hi, gain})
			}
		}
	}
	return mix
}
