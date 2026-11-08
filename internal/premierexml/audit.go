package premierexml

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"opensync/internal/projectstate"
)

// TimelineAudit describes how completely an imported Premiere sequence
// represents the project's sources. It does not certify acoustic alignment.
type TimelineAudit struct {
	Version                  int      `json:"version"`
	Status                   string   `json:"status"`
	ProjectRecordings        int      `json:"project_recordings"`
	MatchedRecordings        int      `json:"matched_recordings"`
	MissingRecordingIDs      []string `json:"missing_recording_ids"`
	UnmatchedVideoClipIDs    []string `json:"unmatched_video_clip_ids"`
	AmbiguousRecordingIDs    []string `json:"ambiguous_recording_ids"`
	VideoClipInstances       int      `json:"video_clip_instances"`
	AudioClipInstances       int      `json:"audio_clip_instances"`
	VideoTracks              int      `json:"video_tracks"`
	AudioTracks              int      `json:"audio_tracks"`
	OverlappingTrackPairs    []string `json:"overlapping_track_pairs"`
	InvalidTimingClipIDs     []string `json:"invalid_timing_clip_ids"`
	RecordingsWithMultiTakes []string `json:"recordings_with_multiple_timeline_instances"`
}

// AuditTimeline compares a PluralEyes-compatible FCP7 XML sequence to an
// existing OpenSync project. Filename matching is explicitly provisional:
// equal basenames from separate directories are ambiguous identities.
func AuditTimeline(source []byte, p *projectstate.Project) (TimelineAudit, error) {
	if err := p.Validate(); err != nil {
		return TimelineAudit{}, err
	}
	slots, err := parseSlots(source)
	if err != nil {
		return TimelineAudit{}, err
	}
	a := TimelineAudit{
		Version: 1, Status: "STRUCTURAL_AUDIT_NOT_ACOUSTIC_CERTIFICATION",
		ProjectRecordings: len(p.Recordings),
		MissingRecordingIDs: []string{}, UnmatchedVideoClipIDs: []string{},
		AmbiguousRecordingIDs: []string{}, OverlappingTrackPairs: []string{},
		InvalidTimingClipIDs: []string{}, RecordingsWithMultiTakes: []string{},
	}
	normalize := func(s string) string {
		s = strings.ReplaceAll(s, "\\", "/")
		return strings.ToLower(path.Base(s))
	}
	byName := map[string][]string{}
	for id, rec := range p.Recordings {
		key := normalize(rec.SourcePath)
		byName[key] = append(byName[key], id)
	}
	for _, ids := range byName {
		sort.Strings(ids)
	}
	matches := map[string]int{}
	seen := map[string]bool{}
	videoTracks, audioTracks := map[int]bool{}, map[int]bool{}
	clipIDs := make([]string, 0, len(slots))
	for id := range slots {
		clipIDs = append(clipIDs, id)
	}
	sort.Strings(clipIDs)
	for _, id := range clipIDs {
		s := slots[id]
		if s.Start < 0 || s.End <= s.Start || s.In < 0 || s.Out <= s.In {
			a.InvalidTimingClipIDs = append(a.InvalidTimingClipIDs, id)
		}
		if s.Media == "audio" {
			a.AudioClipInstances++
			audioTracks[s.Track] = true
			continue
		}
		if s.Media != "video" {
			return TimelineAudit{}, fmt.Errorf("unsupported media type %q", s.Media)
		}
		a.VideoClipInstances++
		videoTracks[s.Track] = true
		ids := byName[normalize(s.Name)]
		switch len(ids) {
		case 0:
			a.UnmatchedVideoClipIDs = append(a.UnmatchedVideoClipIDs, id)
		case 1:
			matches[ids[0]]++
		default:
			for _, rid := range ids {
				seen[rid] = true
			}
		}
	}
	a.VideoTracks, a.AudioTracks = len(videoTracks), len(audioTracks)
	ids := make([]string, 0, len(p.Recordings))
	for id := range p.Recordings {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		key := normalize(p.Recordings[id].SourcePath)
		if len(byName[key]) != 1 && seen[id] {
			a.AmbiguousRecordingIDs = append(a.AmbiguousRecordingIDs, id)
			continue
		}
		switch matches[id] {
		case 0:
			a.MissingRecordingIDs = append(a.MissingRecordingIDs, id)
		default:
			a.MatchedRecordings++
			if matches[id] > 1 {
				a.RecordingsWithMultiTakes = append(a.RecordingsWithMultiTakes, id)
			}
		}
	}
	// Detect pre-existing lane collisions, rather than attributing them
	// to the synchronization engine. Adjacent boundaries are legal.
	for i, aid := range clipIDs {
		x := slots[aid]
		for _, bid := range clipIDs[i+1:] {
			y := slots[bid]
			if x.Media == y.Media && x.Track == y.Track &&
				x.Start < y.End && y.Start < x.End {
				a.OverlappingTrackPairs = append(a.OverlappingTrackPairs, aid+" / "+bid)
			}
		}
	}
	return a, nil
}
