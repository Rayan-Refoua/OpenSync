package autosync

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"math"
	"os"
	"testing"
)

func TestCanonicalRateNeverInfersNTSCFromDenominatorAlone(t *testing.T) {
	cases := map[string]FrameRate{"478125/15937": {30, false}, "113670000/3788881": {30, false}, "1290723764/43023127": {30, false}, "1382053553/46067807": {30, false}, "760250000/25340961": {30, false}, "246798000/8226599": {30, false}, "30031/1001": {30, false}, "30000/1001": {30, true}, "24000/1001": {24, true}, "60000/1001": {60, true}, "50/1": {50, false}}
	for raw, want := range cases {
		got, err := canonicalRate(raw)
		if err != nil || got != want {
			t.Errorf("%s: %v %v, want %v", raw, got, err, want)
		}
		n := xn("clip", nil)
		rateXML(n, got)
		actual, e := xmlRate(n)
		if e != nil || actual != got {
			t.Fatal("rate changed during serialization")
		}
	}
	if _, err := canonicalRate("27.47"); err == nil {
		t.Fatal("unrepresentable source rate silently accepted")
	}
}

func TestLocalOnsetPreviewStaysBesideItsReference(t *testing.T) {
	p := &NativeProject{Data: NativePayload{Name: "local preview", Graph: map[string]any{
		"provisional_placements": []any{map[string]any{
			"recording": 2, "reference": 1, "component": 2, "start_seconds": 12.,
			"status": "provisional_onset_review_required", "timing_basis": "local_onset_consensus",
		}},
	}}}
	for i, status := range []string{"acoustically_supported_not_certified", "unresolved_no_supported_relation", "unresolved_no_supported_relation"} {
		id := string(rune('a' + i))
		p.Data.Records = append(p.Data.Records, MediaRecord{ID: id, Name: id + ".mp4", Source: "C:/media/" + id + ".mp4", Duration: 30,
			Video: map[string]any{"avg_frame_rate": "50/1", "nb_frames": "1500", "width": 1920, "height": 1080}})
		p.Data.Models = append(p.Data.Models, TimeModel{Recording: i, Component: i + 1, Rate: 1, Error: .001, Status: status})
	}
	for _, policy := range []string{"separate-sequence", "append"} {
		o := NativeExportOptions{Unsynced: policy, MaxResidualMS: 150, IncludeProvisional: true}
		placements, rate, err := nativePlacements(p, o)
		if err != nil {
			t.Fatal(err)
		}
		if len(placements) != 3 || placements[2].Sequence != placements[1].Sequence || placements[2].Component != placements[1].Component ||
			placements[2].Start-placements[1].Start != roundFrame(12*rate.FPS()) {
			t.Fatalf("preview lost its reference component: %+v", placements)
		}
		xmlData, err := buildNativeXML(p, placements, rate)
		if err != nil {
			t.Fatal(err)
		}
		if err = ValidateNativeExport(xmlData, p.Data.Records, placements, rate, policy); err != nil {
			t.Fatal(err)
		}
	}
	placements, _, err := nativePlacements(p, NativeExportOptions{Unsynced: "append", MaxResidualMS: 150})
	if err != nil || placements[2].Component != 3 || placements[2].Status != "unresolved_no_supported_relation" {
		t.Fatalf("default export accepted a preview placement: %v, %+v", err, placements)
	}
}
func TestExactSixRecordingCCTVExportRegression(t *testing.T) {
	data, err := os.ReadFile("testdata/zendegifakher-export.json")
	if err != nil {
		t.Fatal(err)
	}
	p := &NativeProject{}
	if err = json.Unmarshal(data, &p.Data); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(p.Data.Models)
	o := NativeExportOptions{Unsynced: "append", MaxResidualMS: 150}
	placements, rate, err := nativePlacements(p, o)
	if err != nil {
		t.Fatal(err)
	}
	if rate != (FrameRate{30, false}) {
		t.Fatalf("wrong sequence rate: %+v", rate)
	}
	xmlData, err := buildNativeXML(p, placements, rate)
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateNativeExport(xmlData, p.Data.Records, placements, rate, o.Unsynced); err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, v := range placements {
		seen[v.Recording] = true
		if v.Recording == 5 {
			if v.Out != 246798 || v.End-v.Start != 246798 {
				t.Fatalf("CCTV arithmetic changed: %+v", v)
			}
		}
	}
	if len(seen) != 6 {
		t.Fatal("missing recording")
	}
	after, _ := json.Marshal(p.Data.Models)
	if !bytes.Equal(before, after) {
		t.Fatal("export altered synchronization evidence")
	}
	broken := bytes.Replace(xmlData, []byte("<ntsc>FALSE</ntsc>"), []byte("<ntsc>TRUE</ntsc>"), 1)
	if ValidateNativeExport(broken, p.Data.Records, placements, rate, o.Unsynced) == nil {
		t.Fatal("accepted inconsistent rate")
	}
}
func TestMixedRatesFractionalPlacementAndSeparateOverlapLanes(t *testing.T) {
	p := &NativeProject{Data: NativePayload{Name: "mixed"}}
	for i, raw := range []string{"30/1", "30/1", "24000/1001"} {
		id := string(rune('a' + i))
		p.Data.Records = append(p.Data.Records, MediaRecord{ID: id, Name: id + ".mp4", Source: "C:/media/" + id + ".mp4", Duration: 50, Video: map[string]any{"avg_frame_rate": raw, "nb_frames": "1200", "width": 1920, "height": 1080}, Audio: map[string]any{"channels": 2, "sample_rate": "48000"}})
		p.Data.Models = append(p.Data.Models, TimeModel{Recording: i, Component: 1, ComponentSize: 3, Offset: float64(i) * .0123, Rate: 1, Error: .001, Status: "acoustically_supported_not_certified"})
	}
	o := NativeExportOptions{Unsynced: "append", MaxResidualMS: 150}
	placements, rate, err := nativePlacements(p, o)
	if err != nil {
		t.Fatal(err)
	}
	data, err := buildNativeXML(p, placements, rate)
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateNativeExport(data, p.Data.Records, placements, rate, o.Unsynced); err != nil {
		t.Fatal(err)
	}
	var root xnode
	if err = xml.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	video := root.get("project/children/sequence/media/video")
	tracks := 0
	for _, c := range video.Children {
		if c.Name == "track" {
			tracks++
		}
	}
	if tracks != 3 {
		t.Fatal("overlapping clips did not get separate tracks")
	}
	for _, v := range placements {
		delta := float64(v.End-v.Start)/rate.FPS() - float64(v.Out-v.In)/v.rate.FPS()
		if math.Abs(delta) > .5/rate.FPS()+1e-9 {
			t.Fatal("mixed-rate duration mismatch")
		}
	}
	placements[0].Out++
	if ValidateNativeExport(data, p.Data.Records, placements, rate, o.Unsynced) == nil {
		t.Fatal("accepted wrong source boundary")
	}
}
func TestNativeMappingCompositionAndFrameCoverage(t *testing.T) {
	parent := []TimePoint{{0, 10}, {30, 40.1}, {80, 90}}
	child := []TimePoint{{0, 5}, {20, 25.02}, {60, 65}}
	combined := ComposeMapping(parent, child, 60)
	for i := 0; i <= 300; i++ {
		v := float64(i) / 5
		if math.Abs(MapTime(combined, v)-MapTime(parent, MapTime(child, v))) > 1e-9 {
			t.Fatal("composition lost timing bend")
		}
	}
	parts, err := SegmentVideo([]TimePoint{{0, 1}, {40, 41.25}, {80, 80.8}, {120, 121.1}}, 6000, 50, .015)
	if err != nil {
		t.Fatal(err)
	}
	next := int64(0)
	for _, p := range parts {
		if p.First != next || p.Last <= p.First || p.Error > .015 {
			t.Fatal("video source frame coverage failed")
		}
		next = p.Last
	}
	if next != 6000 {
		t.Fatal("omitted source frames")
	}
}
