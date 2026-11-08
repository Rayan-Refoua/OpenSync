package enginebench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opensync/internal/researchmatch"
	"opensync/internal/syntheticbench"
)

func statusOf(t *testing.T, checks []Check, name string) string {
	t.Helper()
	for _, c := range checks {
		if c.Name == name {
			return c.Status
		}
	}
	t.Fatalf("missing check %s", name)
	return ""
}

func TestScoreWrongWinnerCannotHideBehindCandidateRecall(t *testing.T) {
	r := Run{Label: "matched", ToleranceMS: 4, ExpectedOffsetsNS: []int64{1e9}, Peaks: []researchmatch.Peak{{OffsetNS: 9e9}, {OffsetNS: 1e9}}}
	Score(&r, false)
	if statusOf(t, r.Checks, "top_one_offset") != "fail" || statusOf(t, r.Checks, "all_valid_alternatives") != "pass" {
		t.Fatalf("wrong winner hidden: %+v", r)
	}
	r.Peaks = nil
	Score(&r, false)
	if r.TopErrorMS != nil || statusOf(t, r.Checks, "top_one_offset") != "fail" {
		t.Fatal("stale score reused")
	}
}

func TestScoreRequiresEveryRepeatedAlignment(t *testing.T) {
	r := Run{Label: "ambiguous", ToleranceMS: 4, ExpectedOffsetsNS: []int64{0, 24e9}, Peaks: []researchmatch.Peak{{OffsetNS: 0}}}
	Score(&r, false)
	if statusOf(t, r.Checks, "all_valid_alternatives") != "fail" {
		t.Fatal("missing repeated event counted as success")
	}
	r.Peaks = append(r.Peaks, researchmatch.Peak{OffsetNS: 24e9})
	Score(&r, false)
	if statusOf(t, r.Checks, "all_valid_alternatives") != "pass" || statusOf(t, r.Checks, "automatic_ambiguity_decision") != "blocked" {
		t.Fatal("recall confused with ambiguity handling")
	}
}

func TestNegativeAndUnreviewedAreNotAccuracySuccesses(t *testing.T) {
	for _, peaks := range [][]researchmatch.Peak{nil, {{OffsetNS: 4e9, Score: 0.99}}} {
		r := Run{Label: "unrelated", Peaks: peaks}
		Score(&r, false)
		if statusOf(t, r.Checks, "automatic_negative_rejection") != "blocked" {
			t.Fatal("unimplemented rejection claimed")
		}
		Score(&r, true)
		want := "pass"
		if len(peaks) > 0 {
			want = "fail"
		}
		if statusOf(t, r.Checks, "silence_has_no_candidates") != want {
			t.Fatal("silence policy wrong")
		}
		r.Label = "unreviewed"
		Score(&r, false)
		if r.TopErrorMS != nil || statusOf(t, r.Checks, "real_ground_truth") != "observation" {
			t.Fatal("unlabelled real audio scored as truth")
		}
	}
}

func TestRegressionAndReadinessGatesDiffer(t *testing.T) {
	r := Report{PrimaryConfiguration: PrimaryMode, Runs: []Run{{Configuration: PrimaryMode, Checks: []Check{{Status: "pass"}}}, {Configuration: "8ms", Checks: []Check{{Status: "fail"}}}}, Checks: []Check{{Status: "blocked"}}}
	Summarize(&r)
	if r.RegressionStatus != "pass" || r.ReadinessStatus != "blocked" {
		t.Fatalf("wrong gates: %+v", r)
	}
	r.Runs[0].Checks[0].Status = "fail"
	Summarize(&r)
	if r.RegressionStatus != "fail" || r.ReadinessStatus != "fail" {
		t.Fatal("primary failure hidden")
	}
}

func TestManifestFailsClosed(t *testing.T) {
	base := Pair{Name: "pair", Reference: "a.wav", Query: "b.wav", Label: "unreviewed"}
	for _, mutate := range []func(*Pair){
		func(p *Pair) { p.ValidOffsetsNS = []int64{1} },
		func(p *Pair) { p.Label = "matched"; p.ValidOffsetsNS = []int64{1}; p.ToleranceMS = 4 },
		func(p *Pair) { p.Label = "unrelated" },
		func(p *Pair) { p.Label = "verified" },
		func(p *Pair) { p.Query = "a.wav" },
		func(p *Pair) {
			p.Label = "ambiguous"
			p.Evidence = "events 1 and 2"
			p.ToleranceMS = 4
			p.ValidOffsetsNS = []int64{0, 0}
		},
	} {
		p := base
		mutate(&p)
		raw, _ := json.Marshal(RealManifest{Version: 1, Pairs: []Pair{p}})
		path := filepath.Join(t.TempDir(), "pairs.json")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadReal(path); err == nil {
			t.Fatalf("bad label accepted: %+v", p)
		}
	}
	path := filepath.Join(t.TempDir(), "valid.json")
	raw, _ := json.Marshal(RealManifest{Version: 1, Pairs: []Pair{base}})
	os.WriteFile(path, raw, 0600)
	m, err := LoadReal(path)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(m.Pairs[0].Reference) {
		t.Fatal("relative media path not resolved")
	}
	os.WriteFile(path, append(raw, []byte(" {}")...), 0600)
	if _, err := LoadReal(path); err == nil {
		t.Fatal("trailing JSON accepted")
	}
}

func TestLocalTimingAndEscapedReport(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "timing")
	fixtures, err := syntheticbench.GenerateTiming(dir, 120)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		r := runTiming(dir, f)
		for _, c := range r.Checks {
			if c.Status != "pass" {
				t.Fatalf("timing regression: %+v", r)
			}
		}
		if len(r.Points) != 5 || r.Points[2].Role != "validation" {
			t.Fatal("validation regions missing")
		}
	}
	out := t.TempDir()
	zero, fractional := 0.0, 4.5
	r := Report{Status: "completed", Runs: []Run{{Scenario: "<script>alert(1)</script>", TopErrorMS: &zero}, {TopErrorMS: &fractional}, {}}}
	if err := writeReport(out, r); err != nil {
		t.Fatal(err)
	}
	if err := writeReport(out, r); err != nil {
		t.Fatal("checkpoint replacement", err)
	}
	raw, err := os.ReadFile(filepath.Join(out, "report.html"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "<script>alert(1)</script>") {
		t.Fatal("HTML unescaped")
	}
	if strings.Contains(string(raw), "%!") || !strings.Contains(string(raw), "0.000 ms") || !strings.Contains(string(raw), "4.500 ms") {
		t.Fatal("HTML timing values not rendered")
	}
	id, err := Identify(filepath.Join(dir, "reference.wav"))
	if err != nil || len(id.SHA256) != 64 || id.Bytes != 132*32000+44 {
		t.Fatalf("bad input identity %+v %v", id, err)
	}
}

func TestBenchmarkRefusesExistingOutputAndInvalidProfile(t *testing.T) {
	if _, err := Execute(Options{Output: t.TempDir(), Profile: "quick"}); err == nil {
		t.Fatal("existing output reused")
	}
	dir := filepath.Join(t.TempDir(), "untouched")
	if _, err := Execute(Options{Output: dir, Profile: "unknown"}); err == nil {
		t.Fatal("invalid profile accepted")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("invalid invocation wrote output")
	}
}
