// Package desktop adapts precise engine projects for read-only presentation.
// Visualization never contributes evidence or changes synchronization models.
package desktop

import (
	"encoding/json"
	"fmt"
	"math"
	"opensync/internal/autosync"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Clip struct {
	ReviewCount          int                  `json:"reviewCount,omitempty"`
	PlacementUncertainty float64              `json:"placementUncertainty,omitempty"`
	TimingBasis          string               `json:"timingBasis,omitempty"`
	ID                   string               `json:"id"`
	Index                int                  `json:"index"`
	Name                 string               `json:"name"`
	Source               string               `json:"source"`
	Duration             float64              `json:"duration"`
	Start                float64              `json:"start"`
	End                  float64              `json:"end"`
	Track                int                  `json:"track"`
	Status               string               `json:"status"`
	State                string               `json:"state"`
	Component            int                  `json:"component"`
	Residual             float64              `json:"residual"`
	Rate                 float64              `json:"rate"`
	Mapping              []autosync.TimePoint `json:"mapping"`
	AudioOrigin          float64              `json:"audioOrigin"`
	Video                bool                 `json:"video"`
	Audio                bool                 `json:"audio"`
	Missing              bool                 `json:"missing"`
	Metadata             autosync.MediaRecord `json:"metadata"`
}
type Track struct {
	Name  string `json:"name"`
	Clips []int  `json:"clips"`
}
type Project struct {
	ManualCandidates []autosync.ManualCandidate `json:"manualCandidates"`
	Name             string                     `json:"name"`
	Path             string                     `json:"path"`
	Generation       uint64                     `json:"generation"`
	Duration         float64                    `json:"duration"`
	FPS              float64                    `json:"fps"`
	Clips            []Clip                     `json:"clips"`
	Tracks           []Track                    `json:"tracks"`
	Evidence         json.RawMessage            `json:"evidence"`
	Graph            map[string]any             `json:"graph"`
}

func LoadProject(path string) (*Project, error) {
	p, err := autosync.LoadNativeProject(path)
	if err != nil {
		return nil, err
	}
	v := BuildProject(p.Data)
	v.Path = path
	var extra struct {
		Evidence json.RawMessage `json:"validated_relations"`
	}
	if err := json.Unmarshal(p.Payload, &extra); err != nil {
		return nil, err
	}
	v.Evidence = extra.Evidence
	return v, nil
}

// BuildProject keeps binary64 timing and monotonic knots. Disconnected components
// are appended for display, without claiming that they share a time origin.
func BuildProject(p autosync.NativePayload) *Project {
	v := &Project{Name: p.Name, FPS: 50, Clips: []Clip{}, Tracks: []Track{}, Graph: p.Graph}
	v.ManualCandidates = autosync.ManualCandidates(p)
	reviewCounts := map[int]int{}
	decisions, _ := p.Graph["manual_candidate_decisions"].(map[string]any)
	for _, candidate := range v.ManualCandidates {
		if decisions[candidate.ID] == nil {
			reviewCounts[candidate.Query]++
		}
	}
	models := map[int]autosync.TimeModel{}
	for _, m := range autosync.ReviewTimingModels(p) {
		models[m.Recording] = m
	}
	groups := map[int][]int{}
	var synced []int
	for i, r := range p.Records {
		m, ok := models[i]
		if !ok {
			m = autosync.TimeModel{Recording: i, Rate: 1, Component: i + 1, Status: "unresolved_pending_analysis"}
		}
		points := append([]autosync.TimePoint(nil), m.Knots...)
		if len(points) < 2 {
			points = []autosync.TimePoint{{0, m.Offset}, {r.Duration, m.Offset + m.Rate*r.Duration}}
		}
		state := "supported"
		if strings.HasPrefix(m.Status, "provisional_") {
			state = "provisional"
		} else if strings.HasPrefix(m.Status, "unresolved") || !m.Resolved {
			state = "unresolved"
		} else if m.Status == "certified" || m.Status == "validated" {
			state = "validated"
		}
		source := r.Source
		if !exists(source) && exists(r.LocalSource) {
			source = r.LocalSource
		}
		c := Clip{ID: r.ID, Index: i, Name: r.Name, Source: source, Duration: r.Duration, Start: autosync.MapTime(points, 0), End: autosync.MapTime(points, r.Duration), Status: m.Status, State: state, Component: m.Component, Residual: m.Error, Rate: m.Rate, Mapping: points, AudioOrigin: m.AudioOrigin, Video: r.Video != nil, Audio: r.Audio != nil, Missing: !exists(source), Metadata: r}
		v.Clips = append(v.Clips, c)
		v.Clips[len(v.Clips)-1].ReviewCount = reviewCounts[i]
		if m.Status == "manually_accepted" {
			v.Clips[len(v.Clips)-1].State = "manual"
		}
		v.Clips[len(v.Clips)-1].PlacementUncertainty = m.PlacementUncertainty
		v.Clips[len(v.Clips)-1].TimingBasis = m.TimingBasis
		if state == "unresolved" || (state == "provisional" && !m.Resolved) {
			groups[m.Component] = append(groups[m.Component], i)
		} else {
			synced = append(synced, i)
		}
	}
	move := func(ids []int, cursor float64) float64 {
		if len(ids) == 0 {
			return cursor
		}
		first := math.Inf(1)
		for _, i := range ids {
			first = math.Min(first, v.Clips[i].Start)
		}
		end := cursor
		for _, i := range ids {
			c := &v.Clips[i]
			delta := cursor - first
			c.Start += delta
			c.End += delta
			for j := range c.Mapping {
				c.Mapping[j][1] += delta
			}
			end = math.Max(end, c.End)
		}
		return end
	}
	cursor := move(synced, 0)
	if len(synced) > 0 && len(groups) > 0 {
		cursor += 10
	}
	keys := make([]int, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := groups[keys[i]], groups[keys[j]]
		av, bv := false, false
		for _, k := range a {
			av = av || v.Clips[k].Video
		}
		for _, k := range b {
			bv = bv || v.Clips[k].Video
		}
		if av != bv {
			return av
		}
		return a[0] < b[0]
	})
	for _, k := range keys {
		cursor = move(groups[k], cursor) + 1
	}
	// Interval partitioning within source groups: overlapping recordings always
	// receive separate lanes. No clip is trimmed to make it fit a track.
	order := make([]int, len(v.Clips))
	for i := range order {
		order[i] = i
	}
	type laneGroup struct{ parent, family string }
	trackKeys := make([]laneGroup, len(v.Clips))
	for i, c := range v.Clips {
		source := c.Metadata.Source
		if source == "" {
			source = c.Source
		}
		trackKeys[i] = laneGroup{filepath.Dir(source), autosync.TrackFamily(c.Name)}
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := trackKeys[order[i]], trackKeys[order[j]]
		if a.parent != b.parent {
			return a.parent < b.parent
		}
		if a.family != b.family {
			return a.family < b.family
		}
		return v.Clips[order[i]].Start < v.Clips[order[j]].Start
	})
	laneEnds := []float64{}
	laneGroups := []laneGroup{}
	for _, i := range order {
		c := &v.Clips[i]
		group := trackKeys[i]
		lane := -1
		for j, g := range laneGroups {
			if g == group && laneEnds[j] <= c.Start+1e-9 {
				lane = j
				break
			}
		}
		if lane < 0 {
			lane = len(v.Tracks)
			v.Tracks = append(v.Tracks, Track{Name: fmt.Sprintf("%02d  %s", lane+1, trackFamilyLabel(group.parent, group.family)), Clips: []int{}})
			laneEnds = append(laneEnds, 0)
			laneGroups = append(laneGroups, group)
		}
		c.Track = lane
		laneEnds[lane] = c.End
		v.Tracks[lane].Clips = append(v.Tracks[lane].Clips, i)
		v.Duration = math.Max(v.Duration, c.End)
	}
	var longest float64
	for _, track := range v.Tracks {
		byRate := map[autosync.FrameRate]float64{}
		var occupied float64
		for _, index := range track.Clips {
			clip := v.Clips[index]
			if !clip.Video {
				continue
			}
			duration := max(0, clip.End-clip.Start)
			occupied += duration
			if rate, err := autosync.VideoFrameRate(clip.Metadata); err == nil {
				byRate[rate] += duration
			}
		}
		if occupied <= longest || len(byRate) == 0 {
			continue
		}
		longest = occupied
		var best float64
		for rate, seconds := range byRate {
			if seconds > best || (seconds == best && rate.FPS() > v.FPS) {
				best, v.FPS = seconds, rate.FPS()
			}
		}
	}
	return v
}
func trackFamilyLabel(parent, family string) string {
	label := filepath.Base(parent)
	if family != "" {
		label += " · " + strings.TrimSuffix(family, filepath.Ext(family))
	}
	return label
}
func exists(path string) bool { st, e := os.Stat(path); return e == nil && !st.IsDir() }
