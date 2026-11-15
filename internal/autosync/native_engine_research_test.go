package autosync

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestChronologyCalibratesCompletionDates(t *testing.T) {
	r, l, _ := chronologyFixture()
	r[0].Duration = 5
	r[0].Video["duration"] = "5"
	l[0].Knots = []TimePoint{{0, 0}, {5, 5}}
	r[2].Duration = 9
	r[2].Video["duration"] = "9"
	l[2].Knots = []TimePoint{{0, 24}, {9, 33}}
	for i := range r {
		r[i].SourceMtime += int64(r[i].Duration * 1e9)
	}
	p := chronologicalPlacements(r, l, nil)
	if len(p) != 1 || p[0].Basis != "calibrated_filesystem_mtime_end" || math.Abs(p[0].Start-12) > .001 {
		t.Fatal(p)
	}
	// A copied timestamp on B alone must not be rescued by the order fallback.
	r[1].SourceMtime += int64(time.Hour)
	if p := chronologicalPlacements(r, l, nil); len(p) != 0 {
		t.Fatal("isolated rewritten date accepted", p)
	}
}

func chronologyFixture() ([]MediaRecord, []NativeLayout, map[string]any) {
	var records []MediaRecord
	var layout []NativeLayout
	for i, start := range []float64{0, 12, 24, 40} {
		r := MediaRecord{ID: stringValue(i), Name: "C000" + stringValue(i+1) + ".mp4", SourceGroup: "camera-a", Duration: 10, SourceMtime: int64(100000+start) * 1e9, Video: map[string]any{"duration": "10", "avg_frame_rate": "50/1", "nb_frames": "500"}}
		r.Source = "C:/media/" + r.Name
		records = append(records, r)
		l := NativeLayout{Recording: i, Component: 1, ComponentSize: 3, Start: start, Rate: 1, Error: .001, Status: "acoustically_supported_not_certified", Knots: []TimePoint{{0, start}, {10, start + 10}}}
		if i == 1 {
			l.Component = 2
			l.ComponentSize = 1
			l.Status = "unresolved_no_supported_relation"
			l.Start = 0
			l.Knots = []TimePoint{{0, 0}, {10, 10}}
		}
		layout = append(layout, l)
	}
	return records, layout, map[string]any{"components": [][]int{{0, 2, 3}, {1}}}
}

func TestChronologicalPlacementIsBoundedAndProvisional(t *testing.T) {
	for _, name := range []string{"wrong_clock", "identical_dates_small_gap", "identical_dates_large_gap", "clock_reset", "ambiguous", "take_order", "different_series", "missing_number", "audio_less"} {
		t.Run(name, func(t *testing.T) {
			r, l, g := chronologyFixture()
			blocked := map[int]bool{}
			switch name {
			case "identical_dates_small_gap", "missing_number":
				for i := range r {
					r[i].SourceMtime = 100000e9
				}
				l[2].Start = 21
				l[2].Knots = []TimePoint{{0, 21}, {10, 31}}
				if name == "missing_number" {
					r[1].Name = "C0099.mp4"
				}
			case "identical_dates_large_gap":
				for i := range r {
					r[i].SourceMtime = 100000e9
				}
			case "clock_reset":
				r[3].SourceMtime += int64(time.Hour)
			case "ambiguous":
				blocked[1] = true
			case "take_order":
				l[2].Start = 2
				l[2].Knots = []TimePoint{{0, 2}, {10, 12}}
			case "different_series":
				r[1].SourceGroup = "camera-b"
			case "audio_less":
				r[1].Audio = nil
			}
			p := chronologicalPlacements(r, l, blocked)
			want := name == "wrong_clock" || name == "identical_dates_small_gap" || name == "audio_less"
			if (len(p) == 1) != want {
				t.Fatalf("unexpected proposals %+v", p)
			}
			if want && (p[0].Uncertainty < 1 || p[0].Status != "provisional_chronological_estimate" || p[0].Start < 10 || p[0].Start+10 > l[2].Start) {
				t.Fatal(p)
			}
			if name == "wrong_clock" {
				applied := applyChronology(r, l, g)
				path := filepath.Join(t.TempDir(), "test.opensync")
				if err := saveNativeProject(path, "chronology", r, nil, applied, g); err != nil {
					t.Fatal(err)
				}
				project, err := LoadNativeProject(path)
				if err != nil {
					t.Fatal(err)
				}
				if project.Data.Models[1].PlacementUncertainty < 1 || project.Data.Models[1].TimingBasis == "acoustic_arrival" {
					t.Fatal("estimate precision hidden")
				}
				placements, rate, err := nativePlacements(project, NativeExportOptions{Output: t.TempDir(), Unsynced: "append", MaxResidualMS: 150})
				if err != nil {
					t.Fatal(err)
				}
				if placements[1].Sequence != "synced" || placements[1].PlacementUncertainty < 1 || placements[1].Out-placements[1].In != 500 {
					t.Fatal(placements[1])
				}
				xml, err := buildNativeXML(project, placements, rate)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(xml), "Chronological estimate") || !strings.Contains(string(xml), "not acoustically verified") {
					t.Fatal("export hid metadata origin")
				}
			}
		})
	}
}

func TestEngineSavedEvidenceReplay(t *testing.T) {
	manifest := os.Getenv("OPENSYNC_RESEARCH_PROJECTS")
	if manifest == "" {
		t.Skip("explicit project manifest required")
	}
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	if err = json.Unmarshal(data, &paths); err != nil {
		t.Fatal(err)
	}
	var reports []map[string]any
	for _, path := range paths {
		p, err := LoadNativeProject(path)
		if err != nil {
			t.Fatal(err)
		}
		var payload struct {
			Edges []*PairEvidence `json:"validated_relations"`
		}
		if err = json.Unmarshal(p.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		var profiles []NativeProfilePair
		for _, e := range payload.Edges {
			if e.Profile != nil {
				profiles = append(profiles, NativeProfilePair{Reference: e.Reference, Query: e.Query, Profiles: []*NativeProfile{e.Profile}})
			}
		}
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		start := time.Now()
		selected, decisions := selectNativeEdges(p.Data.Records, payload.Edges, profiles)
		layout, graph, err := nativeSolve(p.Data.Records, selected)
		if err != nil {
			t.Fatal(err)
		}
		elapsed := time.Since(start)
		runtime.ReadMemStats(&after)
		maxDelta, changed := 0., 0
		for _, m := range layout {
			for _, old := range p.Data.Models {
				if old.Recording == m.Recording {
					delta := math.Abs(m.Start - old.AnalysisOffset)
					maxDelta = math.Max(maxDelta, delta)
					if delta > .001 || old.Status != m.Status {
						changed++
					}
				}
			}
		}
		graph["event_decisions"] = decisions
		chronology := chronologicalPlacements(p.Data.Records, layout, map[int]bool{})
		reports = append(reports, map[string]any{"project": path, "recordings": len(p.Data.Records), "stored_edges": len(payload.Edges), "selected_edges": len(selected), "changed_placements": changed, "maximum_start_difference_seconds": maxDelta, "milliseconds": float64(elapsed.Microseconds()) / 1000, "allocated_bytes": after.TotalAlloc - before.TotalAlloc, "chronological_proposals": chronology, "decisions": decisions, "profile_coverage": "selected profiles only; discarded search hypotheses unavailable in legacy project"})
	}
	if output := os.Getenv("OPENSYNC_RESEARCH_REPORT"); output != "" {
		if err = writeJSON(output, reports); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range reports {
		t.Log(r["recordings"], r["changed_placements"], r["milliseconds"])
	}
}
