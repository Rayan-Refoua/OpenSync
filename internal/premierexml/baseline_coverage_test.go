package premierexml

import (
	"bytes"
	"testing"
)

func TestBaselineCoverageDoesNotCountUnverifiedPairsAsCertified(t *testing.T) {
	baseline := completePreviewFixture()
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
	plan, err := PairPlan(baseline, "A", "B", 600_000_000)
	if err != nil {
		t.Fatal(err)
	}
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
