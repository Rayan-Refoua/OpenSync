package premierexml

import (
	"bytes"
	"strings"
	"testing"

	"opensync/internal/projectstate"
)

func completePreviewFixture() []byte {
	source := string(pairFixture())
	extra := `<track><clipitem id="ref-a"><name>A</name><start>100</start><end>200</end><in>0</in><out>100</out></clipitem>` +
		`<clipitem id="other-a"><name>C</name><start>300</start><end>320</end><in>0</in><out>20</out></clipitem></track>`
	return []byte(strings.Replace(source, "</audio>", extra+"</audio>", 1))
}

func previewProject(t *testing.T) *projectstate.Project {
	t.Helper()
	p := projectstate.New()
	for _, r := range []projectstate.Recording{
		{ID: "A-id", SourcePath: "C:/camera-one/A", DurationNS: 10_000_000_000},
		{ID: "B-id", SourcePath: "C:/camera-two/B", DurationNS: 10_000_000_000},
		{ID: "C-id", SourcePath: "C:/camera-three/C", DurationNS: 10_000_000_000},
	} {
		if err := p.AddRecording(r); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestFullPreviewManifestPreservesAllSourcesAndLinks(t *testing.T) {
	src := completePreviewFixture()
	plan, err := PairPlan(src, "A", "B", 600_000_000)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := Patch(src, plan)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := BuildFullPreviewManifest(src, preview, previewProject(t), "B-id")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.TotalRecordings != 3 || manifest.DiagnosticPlacements != 1 ||
		manifest.InheritedPlacements != 2 || len(manifest.Placements) != 3 {
		t.Fatalf("bad provenance accounting: %+v", manifest)
	}
	for _, item := range manifest.Placements {
		if len(item.AudioClipIDs) != 1 {
			t.Fatalf("missing audio links: %+v", item)
		}
		if item.RecordingID == "B-id" {
			if item.Status != "diagnostic_measured_not_certified" ||
				item.OriginalStart != 10 || item.PreviewStart != 130 {
				t.Fatalf("unreported diagnostic movement: %+v", item)
			}
		} else if item.Status != "template_inherited_unverified" ||
			item.OriginalStart != item.PreviewStart {
			t.Fatalf("inherited source was given false status: %+v", item)
		}
	}
	if !bytes.Equal(src, completePreviewFixture()) {
		t.Fatal("input fixture modified")
	}
}

func TestFullPreviewManifestRejectsUnaccountedMovement(t *testing.T) {
	src := completePreviewFixture()
	plan, err := PairPlan(src, "A", "B", 600_000_000)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := Patch(src, plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildFullPreviewManifest(src, preview, previewProject(t), "C-id"); err == nil {
		t.Fatal("accepted diagnostic movement attributed to wrong recording")
	}
	if _, err := BuildFullPreviewManifest(src, src, previewProject(t), "B-id"); err == nil {
		t.Fatal("accepted preview with zero measured placements")
	}
}

func TestFullPreviewManifestRejectsBrokenAudioLink(t *testing.T) {
	src := completePreviewFixture()
	plan, err := PairPlan(src, "A", "B", 600_000_000)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := Patch(src, plan)
	if err != nil {
		t.Fatal(err)
	}
	preview = bytes.Replace(preview, []byte(`<clipitem id="target-a"><name>B</name><start>130</start>`),
		[]byte(`<clipitem id="target-a"><name>B</name><start>131</start>`), 1)
	if _, err := BuildFullPreviewManifest(src, preview, previewProject(t), "B-id"); err == nil {
		t.Fatal("accepted broken audio-video timing")
	}
}
