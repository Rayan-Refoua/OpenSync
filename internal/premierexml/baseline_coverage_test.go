package premierexml

import (
	"bytes"
	"testing"
)

func TestBaselineCoverageDoesNotCountUnverifiedPairsAsCertified(t *testing.T) {
	baseline := completePreviewFixture()
	// Construct a genuine baseline overlap between A (100..200)
	// and B (170..190), preserving both linked audio/video slots.
	baseline = bytes.ReplaceAll(baseline,
		[]byte("<start>10</start><end>30</end>"),
		[]byte("<start>170</start><end>190</end>"))
	p := previewProject(t)
	same, err := CompareBaselineCoverage(baseline, baseline, p)
	if err != nil {
		t.Fatal(err)
	}
	if same.BaselineRecordings != 3 || same.CandidateRecordings != 3 ||
		same.BaselineOverlapPairs != 1 || same.RetainedOverlapPairs != 1 ||
		len(same.LostOverlapPairs) != 0 || len(same.NewOverlapPairs) != 0 ||
		same.ChangedVideoPlacements != 0 || same.AcousticCertification != "not_assessed" {
		t.Fatalf("incorrect baseline: %+v", same)
	}
	plan := Plan{Placements: []Placement{
		{ClipID: "target", Media: "video", Track: 2,
			ExpectedStart: 170, ExpectedEnd: 190, NewStart: 10},
		{ClipID: "target-a", Media: "audio", Track: 1,
			ExpectedStart: 170, ExpectedEnd: 190, NewStart: 10},
	}}
	moved, err := Patch(baseline, plan)
	if err != nil {
		t.Fatal(err)
	}
	report, err := CompareBaselineCoverage(baseline, moved, p)
	if err != nil {
		t.Fatal(err)
	}
	if report.BaselineOverlapPairs != 1 || len(report.LostOverlapPairs) != 1 ||
		report.CandidateOverlapPairs != 0 || report.ChangedVideoPlacements != 1 ||
		report.InheritedVideoPlacements != 2 {
		t.Fatalf("unreported overlap regression: %+v", report)
	}
	if report.LostOverlapPairs[0].First != "A-id" || report.LostOverlapPairs[0].Second != "B-id" {
		t.Fatalf("wrong lost pair: %+v", report.LostOverlapPairs)
	}
}

func TestBaselineCoverageRejectsMediaLossAndBrokenAudio(t *testing.T) {
	baseline := completePreviewFixture()
	p := previewProject(t)
	plan, err := PairPlan(baseline, "A", "B", 600_000_000)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := Patch(baseline, plan)
	if err != nil {
		t.Fatal(err)
	}
	// Move a video's first audio slot an extra frame, invalidating the link.
	broken := bytes.Replace(moved, []byte(`<clipitem id="target-a"><name>B</name><start>130</start>`),
		[]byte(`<clipitem id="target-a"><name>B</name><start>131</start>`), 1)
	if bytes.Equal(moved, broken) {
		t.Fatal("test fixture mismatch")
	}
	if _, err := CompareBaselineCoverage(baseline, broken, p); err == nil {
		t.Fatal("accepted broken linked audio")
	}
	if _, err := CompareBaselineCoverage(baseline, []byte("<xmeml/>"), p); err == nil {
		t.Fatal("accepted missing media")
	}
}

func TestBaselineCoverageAllowsValidVideoTrackReassignment(t *testing.T) {
	baseline := completePreviewFixture()
	clip := []byte(`<clipitem id="target"><name>B</name><start>10</start><end>30</end><in>0</in><out>20</out></clipitem>`)
	candidate := bytes.Replace(baseline, clip, nil, 1)
	if bytes.Equal(candidate, baseline) {
		t.Fatal("missing video clip fixture")
	}
	candidate = bytes.Replace(candidate,
		[]byte(`</clipitem></track><track>`),
		append([]byte(`</clipitem>`), append(clip, []byte(`</track><track>`)...)...), 1)
	coverage, err := CompareBaselineCoverage(baseline, candidate, previewProject(t))
	if err != nil {
		t.Fatal(err)
	}
	if coverage.ReassignedVideoTracks != 1 ||
		coverage.ReassignedAudioTracks != 0 ||
		coverage.ChangedVideoPlacements != 0 ||
		coverage.CandidateRecordings != 3 {
		t.Fatalf("valid lane move incorrectly rejected or counted: %+v", coverage)
	}
}
