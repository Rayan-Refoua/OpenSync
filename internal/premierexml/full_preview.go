package premierexml

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"opensync/internal/projectstate"
)

// RecordingPlacement identifies the original source behind one video clip
// and the linked audio slots. An inherited placement is NOT synchronized by
// OpenSync, even when it happens to be correct.
type RecordingPlacement struct {
	RecordingID    string   `json:"recording_id"`
	SourcePath     string   `json:"source_path"`
	VideoClipID    string   `json:"video_clip_id"`
	AudioClipIDs   []string `json:"audio_clip_ids"`
	OriginalStart  int64    `json:"original_start_frame"`
	PreviewStart   int64    `json:"preview_start_frame"`
	DurationFrames int64    `json:"duration_frames"`
	Status         string   `json:"status"`
}

// FullPreviewManifest is a provenance record for a complete Premiere
// sequence, not a declaration that its source recordings are in sync.
type FullPreviewManifest struct {
	Version              int                  `json:"version"`
	Status               string               `json:"status"`
	TotalRecordings      int                  `json:"total_recordings"`
	DiagnosticPlacements int                  `json:"diagnostic_placements"`
	InheritedPlacements  int                  `json:"inherited_placements"`
	Placements           []RecordingPlacement `json:"placements"`
}

// BuildFullPreviewManifest confirms that the output retains all expected
// media, only one requested video source changes position, and every
// corresponding audio clip moves with it. The caller is responsible for
// independently validating the measurement report before applying Patch.
func BuildFullPreviewManifest(original, preview []byte, p *projectstate.Project, diagnosticQueryID string) (FullPreviewManifest, error) {
	if err := p.Validate(); err != nil {
		return FullPreviewManifest{}, err
	}
	if _, ok := p.Recordings[diagnosticQueryID]; !ok {
		return FullPreviewManifest{}, fmt.Errorf("unknown diagnostic source %q", diagnosticQueryID)
	}
	audit, err := AuditTimeline(preview, p)
	if err != nil {
		return FullPreviewManifest{}, err
	}
	if audit.MatchedRecordings != len(p.Recordings) ||
		len(audit.MissingRecordingIDs) != 0 ||
		len(audit.AmbiguousRecordingIDs) != 0 ||
		len(audit.UnmatchedVideoClipIDs) != 0 ||
		len(audit.InvalidTimingClipIDs) != 0 ||
		len(audit.OverlappingTrackPairs) != 0 ||
		len(audit.RecordingsWithMultiTakes) != 0 {
		return FullPreviewManifest{}, fmt.Errorf("preview has missing, ambiguous, overlapping or invalid media")
	}
	before, err := parseSlots(original)
	if err != nil {
		return FullPreviewManifest{}, err
	}
	after, err := parseSlots(preview)
	if err != nil {
		return FullPreviewManifest{}, err
	}
	if len(before) != len(after) {
		return FullPreviewManifest{}, fmt.Errorf("clip inventory changed")
	}
	normalize := func(s string) string {
		return strings.ToLower(path.Base(strings.ReplaceAll(s, "\\", "/")))
	}
	byName := map[string]string{}
	for id, rec := range p.Recordings {
		key := normalize(rec.SourcePath)
		if previous, ok := byName[key]; ok {
			return FullPreviewManifest{}, fmt.Errorf("ambiguous basename for %s and %s", previous, id)
		}
		byName[key] = id
	}
	ids := make([]string, 0, len(before))
	for id := range before {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	placements := map[string]RecordingPlacement{}
	for _, id := range ids {
		b := before[id]
		a, ok := after[id]
		if !ok || a.ID != b.ID || a.Name != b.Name ||
			a.Media != b.Media || a.Track != b.Track ||
			a.In != b.In || a.Out != b.Out ||
			a.End-a.Start != b.End-b.Start {
			return FullPreviewManifest{}, fmt.Errorf("unplanned structural change in clip %q", id)
		}
		if b.Media != "video" {
			continue
		}
		recordingID, ok := byName[normalize(b.Name)]
		if !ok {
			return FullPreviewManifest{}, fmt.Errorf("unmapped video clip %q", id)
		}
		if _, exists := placements[recordingID]; exists {
			return FullPreviewManifest{}, fmt.Errorf("multiple video instances for %s", recordingID)
		}
		state := "template_inherited_unverified"
		if a.Start != b.Start {
			if recordingID != diagnosticQueryID {
				return FullPreviewManifest{}, fmt.Errorf("unintended video movement: %s", recordingID)
			}
			state = "diagnostic_measured_not_certified"
		}
		placements[recordingID] = RecordingPlacement{
			RecordingID: recordingID,
			SourcePath: p.Recordings[recordingID].SourcePath,
			VideoClipID: id,
			AudioClipIDs: []string{},
			OriginalStart: b.Start, PreviewStart: a.Start,
			DurationFrames: b.End-b.Start, Status: state,
		}
	}
	if len(placements) != len(p.Recordings) {
		return FullPreviewManifest{}, fmt.Errorf("not every source has exactly one video clip")
	}
	// Match audio by filename and the original shared video timing.
	// This avoids silently moving an unrelated audio clip with a name
	// collision or different source in/out.
	for _, id := range ids {
		b := before[id]
		if b.Media != "audio" {
			continue
		}
		a := after[id]
		recID, ok := byName[normalize(b.Name)]
		if !ok {
			if a.Start != b.Start || a.End != b.End {
				return FullPreviewManifest{}, fmt.Errorf("unmapped audio was moved: %s", id)
			}
			continue
		}
		video := placements[recID]
		videoBefore := before[video.VideoClipID]
		if b.Start != videoBefore.Start || b.End != videoBefore.End ||
			b.In != videoBefore.In || b.Out != videoBefore.Out {
			if a.Start != b.Start || a.End != b.End {
				return FullPreviewManifest{}, fmt.Errorf("unlinked audio was moved: %s", id)
			}
			continue
		}
		if a.Start != video.PreviewStart || a.End != video.PreviewStart+video.DurationFrames {
			return FullPreviewManifest{}, fmt.Errorf("audio/video desynchronization in clip %s", id)
		}
		video.AudioClipIDs = append(video.AudioClipIDs, id)
		placements[recID] = video
	}
	recordingIDs := make([]string, 0, len(p.Recordings))
	for id := range p.Recordings {
		recordingIDs = append(recordingIDs, id)
	}
	sort.Strings(recordingIDs)
	result := FullPreviewManifest{Version: 1,
		Status: "FULL_SEQUENCE_PREVIEW_MIXED_DIAGNOSTIC_AND_INHERITED",
		TotalRecordings: len(p.Recordings), Placements: make([]RecordingPlacement, 0, len(p.Recordings)),
	}
	for _, id := range recordingIDs {
		item := placements[id]
		sort.Strings(item.AudioClipIDs)
		if len(item.AudioClipIDs) == 0 {
			return FullPreviewManifest{}, fmt.Errorf("missing audio for source %s", id)
		}
		if item.Status == "diagnostic_measured_not_certified" {
			result.DiagnosticPlacements++
		} else {
			result.InheritedPlacements++
		}
		result.Placements = append(result.Placements, item)
	}
	if result.DiagnosticPlacements != 1 {
		return FullPreviewManifest{}, fmt.Errorf("expected exactly one measured diagnostic placement; got %d", result.DiagnosticPlacements)
	}
	return result, nil
}
