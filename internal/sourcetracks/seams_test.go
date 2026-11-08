package sourcetracks

import (
	"testing"

	"opensync/internal/projectstate"
)

func TestCrossDirectorySeam(t *testing.T) {
	p := projectstate.New()
	for _, rec := range []projectstate.Recording{
		{ID: "a", SourcePath: `I:\D2-SD1\C0596.MP4`, DurationNS: 1e9},
		{ID: "b", SourcePath: `I:\D2-SD2\C0597.MP4`, DurationNS: 1e9},
		{ID: "c", SourcePath: `I:\D1-SD1\C2141.MP4`, DurationNS: 1e9},
	} {
		if err := p.AddRecording(rec); err != nil {
			t.Fatal(err)
		}
	}
	seams, err := CrossDirectorySeams(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(seams) != 1 || seams[0].Before != "a" ||
		seams[0].After != "b" ||
		seams[0].Continuity != "unknown" {
		t.Fatalf("incorrect seams: %+v", seams)
	}
}
