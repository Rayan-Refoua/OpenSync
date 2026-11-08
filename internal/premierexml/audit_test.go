package premierexml

import (
	"reflect"
	"testing"

	"opensync/internal/projectstate"
)

func auditFixtureProject(t *testing.T) *projectstate.Project {
	t.Helper()
	p := projectstate.New()
	for _, r := range []projectstate.Recording{
		{ID: "A", SourcePath: "D:/cam1/A", DurationNS: 10_000_000_000},
		{ID: "B", SourcePath: "D:/cam2/B", DurationNS: 10_000_000_000},
		{ID: "C", SourcePath: "D:/cam3/C", DurationNS: 10_000_000_000},
		{ID: "D", SourcePath: "D:/cam4/D", DurationNS: 10_000_000_000},
	} {
		if err := p.AddRecording(r); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestTimelineAuditReportsCoverageAndNoCertification(t *testing.T) {
	a, err := AuditTimeline(pairFixture(), auditFixtureProject(t))
	if err != nil {
		t.Fatal(err)
	}
	if a.ProjectRecordings != 4 || a.MatchedRecordings != 3 ||
		a.VideoClipInstances != 3 || a.AudioClipInstances != 1 ||
		a.VideoTracks != 2 || a.AudioTracks != 1 {
		t.Fatalf("unexpected media inventory: %+v", a)
	}
	if !reflect.DeepEqual(a.MissingRecordingIDs, []string{"D"}) ||
		len(a.OverlappingTrackPairs) != 0 ||
		a.Status != "STRUCTURAL_AUDIT_NOT_ACOUSTIC_CERTIFICATION" {
		t.Fatalf("audit wrongly certified or omitted missing source: %+v", a)
	}
}

func TestTimelineAuditAmbiguousFileNames(t *testing.T) {
	p := auditFixtureProject(t)
	if err := p.AddRecording(projectstate.Recording{
		ID: "B-duplicate", SourcePath: "D:/other-card/B",
		DurationNS: 10_000_000_000,
	}); err != nil {
		t.Fatal(err)
	}
	a, err := AuditTimeline(pairFixture(), p)
	if err != nil {
		t.Fatal(err)
	}
	if a.MatchedRecordings != 2 ||
		!reflect.DeepEqual(a.AmbiguousRecordingIDs, []string{"B", "B-duplicate"}) {
		t.Fatalf("basename ambiguity was hidden: %+v", a)
	}
}

func TestTimelineAuditExistingTrackCollision(t *testing.T) {
	source := []byte(fixture)
	// Two video clips occupy overlapping frames on track 2.
	source = []byte(replaceOnce(string(source), "<start>300</start>", "<start>15</start>"))
	a, err := AuditTimeline(source, auditFixtureProject(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(a.OverlappingTrackPairs) != 1 {
		t.Fatalf("track overlap was not reported: %+v", a)
	}
}

func replaceOnce(s, before, after string) string {
	for i := 0; i+len(before) <= len(s); i++ {
		if s[i:i+len(before)] == before {
			return s[:i] + after + s[i+len(before):]
		}
	}
	return s
}
