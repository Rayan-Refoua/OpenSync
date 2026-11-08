package premierexml

import (
	"strings"
	"testing"
)

func pairFixture() []byte {
	return []byte(strings.Replace(
		fixture,
		"<sequence>",
		"<sequence><rate><ntsc>FALSE</ntsc><timebase>50</timebase></rate>",
		1,
	))
}

func TestPairPlanIncludesVideoAndAudio(t *testing.T) {
	p, err := PairPlan(pairFixture(), "A", "B", 600_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Placements) != 2 {
		t.Fatalf("expected linked video and audio: %+v", p)
	}
	for _, placement := range p.Placements {
		if placement.NewStart != 130 {
			t.Fatalf("incorrect placement: %+v", placement)
		}
	}
	if _, err := Patch(pairFixture(), p); err != nil {
		t.Fatal(err)
	}
}

func TestPairPlanRejectsUnknownSource(t *testing.T) {
	if _, err := PairPlan(pairFixture(), "missing", "B", 600_000_000); err == nil {
		t.Fatal("accepted missing recording")
	}
}

func TestPairPlanRejectsTimelineCollision(t *testing.T) {
	// At 50 fps, +4.1s places B at frame 305,
	// overlapping C on the same video track.
	if _, err := PairPlan(pairFixture(), "A", "B", 4_100_000_000); err == nil {
		t.Fatal("accepted track collision")
	}
}

func TestPairPlanRejectsUnsupportedFrameRate(t *testing.T) {
	bad := strings.Replace(
		string(pairFixture()), "<ntsc>FALSE</ntsc>",
		"<ntsc>TRUE</ntsc>", 1,
	)
	if _, err := PairPlan([]byte(bad), "A", "B", 600_000_000); err == nil {
		t.Fatal("accepted unsupported NTSC rate")
	}
}
