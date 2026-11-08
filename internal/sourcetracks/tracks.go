package sourcetracks

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strings"

	"opensync/internal/projectstate"
)

// Track is a proposed recording-source group, not a confirmed device.
// Directory proximity is grouping evidence, never continuity evidence.
type Track struct {
	ID              string   `json:"id"`
	Directory       string   `json:"directory"`
	IdentityBasis   string   `json:"identity_basis"`
	DeviceConfirmed bool     `json:"device_confirmed"`
	RecordingIDs    []string `json:"recording_ids"`
}

type Neighbor struct {
	TrackID    string `json:"track_id"`
	Before     string `json:"before"`
	After      string `json:"after"`
	Basis      string `json:"basis"`
	Continuity string `json:"continuity"`
}

type Proposal struct {
	Version   int        `json:"version"`
	Status    string     `json:"status"`
	Tracks    []Track    `json:"tracks"`
	Neighbors []Neighbor `json:"neighbor_candidates"`
	Seams     []Neighbor `json:"cross_directory_seams"`
}

func normalizedSource(source string) string {
	return strings.ReplaceAll(source, "\\", "/")
}

func trackID(key string) string {
	digest := sha256.Sum256([]byte(key))
	return "directory-" + hex.EncodeToString(digest[:12])
}

// Build proposes source tracks using directory provenance alone.
//
// Filename order is used only to generate investigation candidates.
// It is not a recording chronology, continuity assertion, or sync edge.
func Build(p *projectstate.Project) (Proposal, error) {
	if p == nil {
		return Proposal{}, fmt.Errorf("nil project")
	}
	if err := p.Validate(); err != nil {
		return Proposal{}, err
	}

	proposal := Proposal{
		Version:   1,
		Status:    "PROVISIONAL_DIRECTORY_GROUPING",
		Tracks:    make([]Track, 0),
		Neighbors: make([]Neighbor, 0),
	}

	byDirectory := make(map[string][]string)
	displayDirectories := make(map[string]string)

	for id, rec := range p.Recordings {
		source := normalizedSource(rec.SourcePath)
		if source == "" || path.Base(source) == "." ||
			path.Base(source) == "/" || !strings.Contains(source, "/") {
			return Proposal{}, fmt.Errorf("recording %q has no usable source directory", id)
		}

		dir := path.Dir(source)
		key := strings.ToLower(dir)
		byDirectory[key] = append(byDirectory[key], id)
		if existing, ok := displayDirectories[key]; !ok || dir < existing {
			displayDirectories[key] = dir
		}
	}

	keys := make([]string, 0, len(byDirectory))
	for key := range byDirectory {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		ids := byDirectory[key]
		sort.Slice(ids, func(i, j int) bool {
			a := strings.ToLower(path.Base(normalizedSource(
				p.Recordings[ids[i]].SourcePath)))
			b := strings.ToLower(path.Base(normalizedSource(
				p.Recordings[ids[j]].SourcePath)))
			if a != b {
				return a < b
			}
			return ids[i] < ids[j]
		})

		track := Track{
			ID:              trackID(key),
			Directory:       displayDirectories[key],
			IdentityBasis:   "common_source_directory",
			DeviceConfirmed: false,
			RecordingIDs:    append([]string(nil), ids...),
		}
		proposal.Tracks = append(proposal.Tracks, track)

		for i := 1; i < len(ids); i++ {
			proposal.Neighbors = append(proposal.Neighbors, Neighbor{
				TrackID:    track.ID,
				Before:     ids[i-1],
				After:      ids[i],
				Basis:      "filename_sort_only",
				Continuity: "unknown",
			})
		}
	}

	seams, err := CrossDirectorySeams(p)
	if err != nil {
		return Proposal{}, err
	}
	proposal.Seams = seams
	return proposal, nil
}
