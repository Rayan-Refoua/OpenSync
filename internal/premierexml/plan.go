package premierexml

import (
	"encoding/xml"
	"fmt"
	"math"
	"sort"
)

// PairPlan derives Premiere placements from a measured relative offset.
// This is diagnostic planning, not evidence certification.
//
// Offset convention:
// start(query) - start(reference) = offsetNS.
func PairPlan(source []byte, referenceName, queryName string, offsetNS int64) (Plan, error) {
	if referenceName == "" || queryName == "" || referenceName == queryName {
		return Plan{}, fmt.Errorf("invalid recording names")
	}

	var document struct {
		Project struct {
			Children struct {
				Sequence struct {
					Rate struct {
						Timebase int    `xml:"timebase"`
						NTSC     string `xml:"ntsc"`
					} `xml:"rate"`
				} `xml:"sequence"`
			} `xml:"children"`
		} `xml:"project"`
	}
	if err := xml.Unmarshal(source, &document); err != nil {
		return Plan{}, err
	}
	rate := document.Project.Children.Sequence.Rate
	if rate.Timebase <= 0 || rate.Timebase > 240 || rate.NTSC != "FALSE" {
		return Plan{}, fmt.Errorf("unsupported sequence frame rate")
	}

	slots, err := parseSlots(source)
	if err != nil {
		return Plan{}, err
	}

	var reference, query *slot
	var queryAudio []slot

	for _, s := range slots {
		if s.Media == "video" && s.Name == referenceName {
			if reference != nil {
				return Plan{}, fmt.Errorf("ambiguous reference video %q", referenceName)
			}
			copy := s
			reference = &copy
		}
		if s.Media == "video" && s.Name == queryName {
			if query != nil {
				return Plan{}, fmt.Errorf("ambiguous query video %q", queryName)
			}
			copy := s
			query = &copy
		}
		if s.Media == "audio" && s.Name == queryName {
			queryAudio = append(queryAudio, s)
		}
	}
	if reference == nil || query == nil || len(queryAudio) == 0 {
		return Plan{}, fmt.Errorf("reference, query, or linked audio not found")
	}

	for _, s := range queryAudio {
		if s.Start != query.Start || s.End != query.End ||
			s.In != query.In || s.Out != query.Out {
			return Plan{}, fmt.Errorf("query audio/video placement mismatch")
		}
	}

	framesFloat := math.Round(float64(offsetNS) * float64(rate.Timebase) / 1e9)
	if math.IsInf(framesFloat, 0) || math.IsNaN(framesFloat) ||
		framesFloat > 1e12 || framesFloat < -1e12 {
		return Plan{}, fmt.Errorf("offset outside supported range")
	}
	newStart := reference.Start + int64(framesFloat)
	if newStart < 0 || newStart > 1e12 {
		return Plan{}, fmt.Errorf("resulting placement outside timeline")
	}

	instances := append([]slot{*query}, queryAudio...)
	sort.Slice(instances, func(i, j int) bool {
		if instances[i].Media != instances[j].Media {
			return instances[i].Media == "video"
		}
		return instances[i].Track < instances[j].Track
	})

	plan := Plan{Placements: make([]Placement, 0, len(instances))}
	for _, s := range instances {
		plan.Placements = append(plan.Placements, Placement{
			ClipID:        s.ID,
			Media:         s.Media,
			Track:         s.Track,
			ExpectedStart: s.Start,
			ExpectedEnd:   s.End,
			NewStart:      newStart,
		})
	}

	// Reuse the exporter's existing collision and source-state checks.
	if _, err := Patch(source, plan); err != nil {
		return Plan{}, fmt.Errorf("generated placement is not exportable: %w", err)
	}
	return plan, nil
}
