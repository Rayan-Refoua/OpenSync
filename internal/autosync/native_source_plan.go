package autosync

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
)

// SourceAssignment is a saved layout decision. Exclusive expresses a user's
// assertion about a recording stream and is used only for the advisory audit.
type SourceAssignment struct {
	Group     string `json:"group"`
	Label     string `json:"label"`
	Exclusive bool   `json:"exclusive,omitempty"`
}

type SourceReviewClip struct {
	MediaType  string           `json:"mediaType"`
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	Path       string           `json:"path"`
	Bytes      int64            `json:"bytes"`
	Mtime      string           `json:"mtime"`
	Format     string           `json:"format"`
	Basis      string           `json:"basis"`
	Assignment SourceAssignment `json:"assignment"`
}

type SourcePlan struct {
	Version  int                `json:"version"`
	Clips    []SourceReviewClip `json:"clips"`
	Warnings []string           `json:"warnings,omitempty"`
}

func sourceInventory(records []MediaRecord) SourcePlan {
	plan := SourcePlan{Version: 1}
	groups := map[string]int{}
	transcoded := 0
	for _, r := range records {
		group, label, basis := detectSourceTrack(r)
		assignment := SourceAssignment{Group: group, Label: label, Exclusive: false}
		if r.SourceAssignment != nil {
			assignment = *r.SourceAssignment
		}
		if assignment.Label == "" {
			assignment.Label = filepath.Base(filepath.Dir(r.Source))
		}
		groups[assignment.Group]++
		if basis == "transcoded" {
			transcoded++
		}
		kind := "audio"
		var format []string
		if r.Video != nil {
			kind = "video"
			format = append(format, fmt.Sprintf("%d×%d %s", int(number(r.Video["width"])), int(number(r.Video["height"])), stringValue(r.Video["codec_name"])))
		}
		for _, stream := range sourceAudioStreams(r) {
			format = append(format, fmt.Sprintf("%s %d Hz · %d ch %s", stringValue(stream["codec_name"]), int(number(stream["sample_rate"])), int(number(stream["channels"])), stringValue(stream["channel_layout"])))
		}
		plan.Clips = append(plan.Clips, SourceReviewClip{
			ID: r.ID, Name: r.Name, Path: r.Source, Bytes: r.SourceBytes, MediaType: kind,
			Mtime: fmt.Sprint(r.SourceMtime), Format: strings.Join(format, " / "),
			Basis: basis, Assignment: assignment,
		})
	}
	if len(groups) == 1 && len(records) > 1 {
		plan.Warnings = append(plan.Warnings, "Only one source group was detected. Split the group if it contains multiple recorders. Matching stays available for overlapping clips.")
	}
	if transcoded > 1 {
		plan.Warnings = append(plan.Warnings, "Several clips have transcoder metadata. Shared encoding settings do not identify their original recorders.")
	}
	for _, clip := range plan.Clips {
		if clip.Basis == "profile" && groups[clip.Assignment.Group] > 1 {
			plan.Warnings = append(plan.Warnings, "Some clips are grouped by their recording profile alone. Different recorders may share that profile.")
			break
		}
	}
	return plan
}

func applySourcePlan(records []MediaRecord, proposed, reviewed SourcePlan) error {
	if reviewed.Version != proposed.Version || len(reviewed.Clips) != len(proposed.Clips) {
		return fmt.Errorf("source plan version or recording count differs")
	}
	byID := make(map[string]SourceReviewClip, len(reviewed.Clips))
	groupSettings := map[string]SourceAssignment{}
	for _, clip := range reviewed.Clips {
		a := clip.Assignment
		if _, exists := byID[clip.ID]; exists || strings.TrimSpace(a.Group) == "" || len(a.Group) > 1024 || len(a.Label) > 256 || strings.ContainsAny(a.Group, "\x00\r\n") {
			return fmt.Errorf("invalid source assignment for %q", clip.Name)
		}
		if previous, ok := groupSettings[a.Group]; ok && previous != a {
			return fmt.Errorf("inconsistent settings for source group %q", a.Label)
		}
		byID[clip.ID], groupSettings[a.Group] = clip, a
	}
	groupMedia := map[string]string{}
	for _, clip := range proposed.Clips {
		reviewedClip, ok := byID[clip.ID]
		if !ok || reviewedClip.Path != clip.Path || reviewedClip.Bytes != clip.Bytes || reviewedClip.Mtime != clip.Mtime {
			return fmt.Errorf("source plan no longer matches %q", clip.Name)
		}
		group := reviewedClip.Assignment.Group
		if kind, ok := groupMedia[group]; ok && kind != clip.MediaType {
			return fmt.Errorf("audio-only and video recordings need separate source groups")
		}
		groupMedia[group] = clip.MediaType
	}
	changedGroups := map[string]bool{}
	for _, clip := range proposed.Clips {
		a := byID[clip.ID].Assignment
		if a != clip.Assignment {
			changedGroups[a.Group] = true
		}
	}
	for i, clip := range proposed.Clips {
		a := byID[clip.ID].Assignment
		if changedGroups[a.Group] {
			records[i].SourceAssignment = &a
		}
	}
	return nil
}

// Count disjoint pairs using conservative intervals. A two-second allowance
// accounts for timestamp granularity. This report never prunes comparisons.
func sourceComparisonAudit(records []MediaRecord, mode string) int64 {
	if mode != "chronological" {
		return 0
	}
	type interval struct{ start, end float64 }
	groups := map[string][]interval{}
	for _, r := range records {
		_, _, basis := detectSourceTrack(r)
		exclusive := basis == "device"
		if r.SourceAssignment != nil {
			exclusive = r.SourceAssignment.Exclusive
		}
		if !exclusive || basis == "transcoded" || r.Audio == nil || r.Duration <= 0 {
			continue
		}
		created, hasCreated := recordingStamp(r, "sidecar_creation")
		if !hasCreated {
			created, hasCreated = recordingStamp(r, "container_creation")
		}
		modified, hasModified := recordingStamp(r, "filesystem_mtime")
		if !hasCreated || !hasModified || math.Min(math.Abs(modified-created), math.Abs(modified-created-r.Duration)) > 2 {
			continue
		}
		key, _ := SourceTrackGroup(r)
		groups[key] = append(groups[key], interval{created - 2, created + r.Duration + 2})
	}
	var count int64
	for _, intervals := range groups {
		ends := make([]float64, len(intervals))
		for i, clip := range intervals {
			ends[i] = clip.end
		}
		sort.Float64s(ends)
		for _, clip := range intervals {
			count += int64(sort.SearchFloat64s(ends, clip.start))
		}
	}
	return count
}
