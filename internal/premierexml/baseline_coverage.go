package premierexml

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"opensync/internal/projectstate"
)

// BaselinePair represents timeline overlap, NOT verified acoustic
// correspondence. The PluralEyes XML is a coverage reference, not truth.
type BaselinePair struct {
	First  string `json:"first"`
	Second string `json:"second"`
}

type BaselineCoverage struct {
	Status                   string         `json:"status"`
	BaselineRecordings       int            `json:"baseline_recordings"`
	CandidateRecordings      int            `json:"candidate_recordings"`
	BaselineOverlapPairs     int            `json:"baseline_overlap_pairs"`
	CandidateOverlapPairs    int            `json:"candidate_overlap_pairs"`
	RetainedOverlapPairs     int            `json:"retained_overlap_pairs"`
	LostOverlapPairs         []BaselinePair `json:"lost_overlap_pairs"`
	NewOverlapPairs          []BaselinePair `json:"new_overlap_pairs"`
	InheritedVideoPlacements int            `json:"inherited_video_placements"`
	ChangedVideoPlacements   int            `json:"changed_video_placements"`
	ReassignedVideoTracks    int            `json:"reassigned_video_tracks"`
	ReassignedAudioTracks    int            `json:"reassigned_audio_tracks"`
	UnchangedAudioVideoLinks bool           `json:"unchanged_audio_video_links"`
	AcousticCertification    string         `json:"acoustic_certification"`
}

func baselinePairs(slots map[string]slot, names map[string]string) (map[BaselinePair]bool, map[string]slot, error) {
	videos := make(map[string]slot)
	for _, s := range slots {
		if s.Media != "video" {
			continue
		}
		name := strings.ToLower(path.Base(strings.ReplaceAll(s.Name, "\\", "/")))
		id, ok := names[name]
		if !ok {
			return nil, nil, fmt.Errorf("unmapped baseline video %q", s.Name)
		}
		if _, exists := videos[id]; exists {
			return nil, nil, fmt.Errorf("duplicate video recording %q", id)
		}
		if s.Start < 0 || s.End <= s.Start {
			return nil, nil, fmt.Errorf("invalid video timing %q", id)
		}
		videos[id] = s
	}
	ids := make([]string, 0, len(videos))
	for id := range videos {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	pairs := make(map[BaselinePair]bool)
	for i, first := range ids {
		a := videos[first]
		for _, second := range ids[i+1:] {
			b := videos[second]
			if a.Start < b.End && b.Start < a.End {
				pairs[BaselinePair{First: first, Second: second}] = true
			}
		}
	}
	return pairs, videos, nil
}

func sortedBaselinePairs(m map[BaselinePair]bool) []BaselinePair {
	result := make([]BaselinePair, 0, len(m))
	for p := range m {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].First != result[j].First {
			return result[i].First < result[j].First
		}
		return result[i].Second < result[j].Second
	})
	return result
}

// CompareBaselineCoverage measures whether an OpenSync candidate preserves
// PluralEyes media and overlap coverage. It intentionally does not interpret
// PluralEyes overlapping clips as verified sync, or label nonoverlap as
// unsyncable. Missing/changed source media and broken audio/video links fail
// closed. Lost overlap is reported for review, not silently suppressed.
func CompareBaselineCoverage(baseline, candidate []byte, p *projectstate.Project) (BaselineCoverage, error) {
	if err := p.Validate(); err != nil {
		return BaselineCoverage{}, err
	}
	original, err := parseSlots(baseline)
	if err != nil {
		return BaselineCoverage{}, err
	}
	next, err := parseSlots(candidate)
	if err != nil {
		return BaselineCoverage{}, err
	}
	if len(original) != len(next) {
		return BaselineCoverage{}, fmt.Errorf("baseline/candidate media inventory differs")
	}
	names := make(map[string]string, len(p.Recordings))
	for id, r := range p.Recordings {
		name := strings.ToLower(path.Base(strings.ReplaceAll(r.SourcePath, "\\", "/")))
		if _, exists := names[name]; exists {
			return BaselineCoverage{}, fmt.Errorf("ambiguous basename %q", name)
		}
		names[name] = id
	}
	for id, a := range original {
		b, ok := next[id]
		if !ok || a.ID != b.ID || a.Name != b.Name || a.Media != b.Media ||
			a.In != b.In || a.Out != b.Out ||
			a.End-a.Start != b.End-b.Start {
			return BaselineCoverage{}, fmt.Errorf("structural/media change in clip %q", id)
		}
		if b.Start < 0 || b.End <= b.Start {
			return BaselineCoverage{}, fmt.Errorf("invalid candidate position %q", id)
		}
	}
	before, baselineVideos, err := baselinePairs(original, names)
	if err != nil {
		return BaselineCoverage{}, err
	}
	after, candidateVideos, err := baselinePairs(next, names)
	if err != nil {
		return BaselineCoverage{}, err
	}
	if len(baselineVideos) != len(p.Recordings) || len(candidateVideos) != len(p.Recordings) {
		return BaselineCoverage{}, fmt.Errorf("baseline or candidate omits project recordings")
	}
	// A recording's originally linked audio must move by exactly the same
	// frames as its video. Unmapped clips may not move.
	changed, inherited := 0, 0
	videoTrackChanges, audioTrackChanges := 0, 0
	for id, a := range original {
		b := next[id]
		if a.Track != b.Track {
			if a.Media == "video" {
				videoTrackChanges++
			} else if a.Media == "audio" {
				audioTrackChanges++
			}
		}
	}
	videoShift := make(map[string]int64, len(baselineVideos))
	for id, a := range baselineVideos {
		b := candidateVideos[id]
		shift := b.Start - a.Start
		videoShift[strings.ToLower(path.Base(strings.ReplaceAll(a.Name, "\\", "/")))] = shift
		if shift == 0 {
			inherited++
		} else {
			changed++
		}
	}
	linkedAudio := make(map[string]int)
	for id, a := range original {
		if a.Media != "audio" {
			continue
		}
		b := next[id]
		key := strings.ToLower(path.Base(strings.ReplaceAll(a.Name, "\\", "/")))
		recID, known := names[key]
		if !known {
			if a.Start != b.Start {
				return BaselineCoverage{}, fmt.Errorf("unmapped audio moved %q", id)
			}
			continue
		}
		video := baselineVideos[recID]
		if a.Start == video.Start && a.End == video.End && a.In == video.In && a.Out == video.Out {
			if b.Start-a.Start != videoShift[key] {
				return BaselineCoverage{}, fmt.Errorf("broken linked audio/video shift %q", id)
			}
			linkedAudio[recID]++
		} else if a.Start != b.Start {
			return BaselineCoverage{}, fmt.Errorf("unlinked audio moved %q", id)
		}
	}
	for id := range baselineVideos {
		if linkedAudio[id] == 0 {
			return BaselineCoverage{}, fmt.Errorf("missing baseline-linked audio for %q", id)
		}
	}
	lost, newPairs := make(map[BaselinePair]bool), make(map[BaselinePair]bool)
	retained := 0
	for pair := range before {
		if after[pair] {
			retained++
		} else {
			lost[pair] = true
		}
	}
	for pair := range after {
		if !before[pair] {
			newPairs[pair] = true
		}
	}
	result := BaselineCoverage{
		Status:                   "BASELINE_OVERLAP_AUDIT_NOT_ACOUSTIC_CERTIFICATION",
		BaselineRecordings:       len(baselineVideos),
		CandidateRecordings:      len(candidateVideos),
		BaselineOverlapPairs:     len(before),
		CandidateOverlapPairs:    len(after),
		RetainedOverlapPairs:     retained,
		LostOverlapPairs:         sortedBaselinePairs(lost),
		NewOverlapPairs:          sortedBaselinePairs(newPairs),
		InheritedVideoPlacements: inherited,
		ChangedVideoPlacements:   changed,
		ReassignedVideoTracks:    videoTrackChanges,
		ReassignedAudioTracks:    audioTrackChanges,
		UnchangedAudioVideoLinks: true,
		AcousticCertification:    "not_assessed",
	}
	return result, nil
}
