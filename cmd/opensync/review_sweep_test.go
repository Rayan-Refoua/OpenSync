package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeSweepFixture(t *testing.T, dir, filename string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestSweepReviewFlagsC0594AndRetainsC0599(t *testing.T) {
	dir := t.TempDir()
	writeSweepFixture(t, dir, "summary.json", map[string]any{
		"total_recordings": 3,
		"results": []any{
			map[string]any{"hypothesis_id": "bad", "reference": "2148", "query": "C0594", "outcome": "measured_diagnostic", "report": `C:\experiment\probe-001.json`},
			map[string]any{"hypothesis_id": "good", "reference": "2161", "query": "C0599", "outcome": "measured_diagnostic", "report": "probe-002.json"},
			map[string]any{"hypothesis_id": "missing", "reference": "2142", "query": "C0553", "outcome": "probe_failed", "report": "probe-003.json"},
		},
	})
	probe := func(id string, correlation, control, spectral float64) map[string]any {
		windows := make([]any, 3)
		for i := range windows {
			windows[i] = map[string]any{
				"measurement": map[string]any{
					"measured_offset_ns": int64(52_711_000_000 + i*1_000_000),
					"correlation":        correlation,
				},
				"spectral": map[string]any{
					"band_correlation": []float64{spectral, spectral, spectral},
				},
				"wrong_offset_controls": []any{
					map[string]any{"correlation": control},
					map[string]any{"correlation": -control},
				},
			}
		}
		return map[string]any{"hypothesis_id": id, "reference": "ref", "query": "query", "probes": windows}
	}
	writeSweepFixture(t, dir, "probe-001.json", probe("bad", 0.02, 0.04, 0.01))
	writeSweepFixture(t, dir, "probe-002.json", probe("good", 0.24, 0.03, 0.31))
	writeSweepFixture(t, dir, "probe-003.json", map[string]any{"error": "insufficient predicted overlap for bounded probes"})
	a, err := reviewSweepFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a.Attempted != 3 || a.Completed != 2 || a.Failed != 1 ||
		a.ControlWarning != 1 || a.NoControlWarning != 1 {
		t.Fatalf("wrong counts: %+v", a)
	}
	if a.Rows[0].HypothesisID != "bad" ||
		a.Rows[0].Classification != "negative_control_warning" ||
		a.Rows[0].ControlOutperformed != 3 {
		t.Fatalf("negative controls did not flag weak relationship: %+v", a.Rows[0])
	}
	var good, missing *SweepReviewRow
	for i := range a.Rows {
		switch a.Rows[i].HypothesisID {
		case "good":
			good = &a.Rows[i]
		case "missing":
			missing = &a.Rows[i]
		}
	}
	if good == nil || good.Classification != "requires_independent_validation" {
		t.Fatalf("good candidate was not preserved: %+v", good)
	}
	if missing == nil || missing.Failure == "" {
		t.Fatalf("failed probe lost its error: %+v", missing)
	}
}

func TestSweepReviewRejectsMissingReports(t *testing.T) {
	dir := t.TempDir()
	writeSweepFixture(t, dir, "summary.json", map[string]any{
		"total_recordings": 2,
		"results": []any{
			map[string]any{"hypothesis_id": "bad", "report": `C:\missing\probe-123.json`, "outcome": "measured_diagnostic"},
		},
	})
	if _, err := reviewSweepFolder(dir); err == nil {
		t.Fatal("silently accepted missing probe report")
	}
}
