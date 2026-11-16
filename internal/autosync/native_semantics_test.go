package autosync

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeModelSelectionRequiresReservedAgreement(t *testing.T) {
	for _, kind := range []string{"constant", "affine", "piecewise_linear"} {
		truth := func(x float64) float64 {
			switch kind {
			case "affine":
				return 3.125 + .001*x
			case "piecewise_linear":
				return 3.125 + .002*math.Min(x, 60) - .002*math.Max(x-60, 0)
			}
			return 3.125
		}
		observations := func(first, last, step float64) []*Measurement {
			out := []*Measurement{}
			for x := first; x <= last; x += step {
				out = append(out, &Measurement{QueryTime: x, Offset: truth(x), Window: 1, Supported: true, Precise: true, Wave: .8, Control: .1})
			}
			return out
		}
		models := fitNativeModels(observations(0, 120, 20))
		var selected *NativeRelationModel
		for _, m := range models {
			if judgeNative(m, observations(10, 110, 20), .025, 2).Passes {
				selected = m
				break
			}
		}
		if selected == nil || selected.Kind != kind {
			t.Fatalf("%s: wrong model %+v", kind, selected)
		}
		audit := observations(5, 115, 20)
		if !judgeNative(selected, audit, .025, 2).Passes {
			t.Fatal("true holdout rejected")
		}
		audit[2].Offset += .1
		if judgeNative(selected, audit, .025, 2).Passes {
			t.Fatal("contradictory holdout accepted")
		}
	}
}
func testRelation(ref, query int, offset, confidence float64) *PairEvidence {
	observations := []*Measurement{}
	for _, x := range []float64{1, 20, 40} {
		observations = append(observations, &Measurement{QueryTime: x, Offset: offset, Window: 1, Supported: true, Precise: true})
	}
	return &PairEvidence{Reference: ref, Query: query, Selected: &CandidateEvidence{NativeCandidate: NativeCandidate{Offset: offset}, Confidence: confidence, Residual: .001, Fit: observations, Span: 39}}
}
func testProfile(offset float64, status string) *NativeProfile {
	obs := []*Measurement{}
	for _, x := range []float64{1, 4, 7} {
		obs = append(obs, &Measurement{QueryTime: x, Offset: offset, Window: .5, Supported: true, Precise: true})
	}
	return &NativeProfile{Status: status, Prior: NativePrior{Offset: offset, Strength: .5}, Model: &NativeRelationModel{Kind: "constant", Offset: offset}, Training: obs, Selection: obs, Audit: obs, Confidence: .5, Range: []float64{0, 9}, Limit: .025}
}
func TestNativeRepeatedProgrammeRequiresCorroboration(t *testing.T) {
	records := []MediaRecord{{ID: "camera", Duration: 300, Video: map[string]any{"avg_frame_rate": "50/1"}}, {ID: "recorder", Duration: 500}, {ID: "short", Duration: 9}}
	base := []*PairEvidence{testRelation(0, 1, 10, 1), testRelation(0, 2, 250, 1)}
	profiles := []NativeProfilePair{{Reference: 0, Query: 2, Profiles: []*NativeProfile{testProfile(250, "validated_relation"), testProfile(40, "event_supported_timing_unresolved")}}, {Reference: 1, Query: 2, Profiles: []*NativeProfile{testProfile(30, "validated_relation")}}}
	selected, decisions := selectNativeEdges(records, base, profiles)
	if len(decisions) == 0 || decisions[0]["status"] != "corroborated_alternative" {
		t.Fatalf("wrong event decision: %+v", decisions)
	}
	layout, _, err := nativeSolve(records, selected)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range layout {
		if m.Recording == 2 && math.Abs(m.Start-40) > 1e-9 {
			t.Fatalf("wrong repeated event: %+v", m)
		}
	}
	profiles[0].Profiles = profiles[0].Profiles[:1]
	selected, decisions = selectNativeEdges(records, base, profiles)
	if decisions[0]["status"] != "unresolved_competing_events" {
		t.Fatal("unresolved competition hidden")
	}
	for _, e := range selected {
		if e.Reference == 2 || e.Query == 2 {
			t.Fatal("ambiguous clip remained connected")
		}
	}
}
func TestNativeExcerptCannotJoinLongRecordings(t *testing.T) {
	records := []MediaRecord{{Duration: 180}, {Duration: 240}, {Duration: 8}}
	excerpt, short := testRelation(0, 1, 100, 1), testRelation(0, 2, 40, 1)
	for _, e := range []*PairEvidence{excerpt, short} {
		e.Selected.Fit = []*Measurement{}
		for _, x := range []float64{1, 3, 7} {
			e.Selected.Fit = append(e.Selected.Fit, &Measurement{QueryTime: x, Offset: e.Selected.Offset, Window: 1})
		}
	}
	selected, rejected := rejectExcerptBridges(records, []*PairEvidence{excerpt, short})
	if len(selected) != 1 || selected[0] != short || len(rejected) != 1 {
		t.Fatal("brief excerpt established unrelated long event")
	}
}
func TestNativePiecewiseGraphRejectsBadCycle(t *testing.T) {
	records := []MediaRecord{{ID: "camera", Duration: 120, Video: map[string]any{"avg_frame_rate": "50/1"}}, {ID: "one", Duration: 120}, {ID: "two", Duration: 120}}
	first := testRelation(0, 1, 7, 1)
	first.Selected.Knots = []TimePoint{{0, 7}, {60, 67.1}, {120, 127}}
	for _, o := range first.Selected.Fit {
		o.Offset = MapTime(first.Selected.Knots, o.QueryTime) - o.QueryTime
	}
	layout, graph, err := nativeSolve(records, []*PairEvidence{first, testRelation(1, 2, 3, 1), testRelation(0, 2, 99, .05)})
	if err != nil {
		t.Fatal(err)
	}
	rejects := graph["rejected_edges"].([]map[string]any)
	if len(rejects) != 1 || rejects[0]["edge"] != 2 {
		t.Fatal("wrong conflicting edge removed")
	}
	for _, m := range layout {
		if m.Recording == 2 && math.Abs(MapTime(m.Knots, 57)-67.1) > 1e-9 {
			t.Fatal("timing bend lost")
		}
	}
}
func TestNativeSonySourceAddressAndDropFrame(t *testing.T) {
	a, err := sonyLTC("19320500", 25, true, FrameRate{50, false})
	if err != nil || int(number(a["frame"])) != 16638 {
		t.Fatalf("C0572 start address: %+v %v", a, err)
	}
	b, err := sonyLTC("07431280", 25, true, FrameRate{50, false})
	if err != nil || number(b["frame"])-number(a["frame"]) != 21527 {
		t.Fatal("HFR parity lost")
	}
	c, err := parseSourceTC("01:00:00;00", FrameRate{30, true})
	if err != nil || number(c["frame"]) != 107892 {
		t.Fatal("drop-frame address wrong")
	}
	if _, err = parseSourceTC("00:01:00;00", FrameRate{30, true}); err == nil {
		t.Fatal("invalid drop-frame address accepted")
	}
	if _, err = sonyLTC("FA000000", 25, false, FrameRate{25, false}); err == nil {
		t.Fatal("invalid BCD accepted")
	}
}
func TestNativeResumePreservesSavedInventoryOrder(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "project")
	os.MkdirAll(filepath.Join(out, "cache"), 0755)
	records := []MediaRecord{}
	files := []string{}
	for _, name := range []string{"b.wav", "A.wav"} {
		source := filepath.Join(root, name)
		if err := writeFixtureWAV(source, make([]int16, 16000)); err != nil {
			t.Fatal(err)
		}
		st, _ := os.Stat(source)
		id := shaBytes([]byte(source))[:20]
		r := MediaRecord{ID: id, Name: name, Source: source, Cache: source, Duration: 1, SourceBytes: st.Size(), SourceMtime: st.ModTime().UnixNano()}
		records = append(records, r)
		files = append(files, source)
		if err := writeJSON(filepath.Join(out, "cache", id+".json"), r); err != nil {
			t.Fatal(err)
		}
	}
	writeJSON(filepath.Join(out, "run-config.json"), map[string]any{"version": "opensync-autonomous-v1", "files": files, "max_residual_ms": 150})
	writeJSON(filepath.Join(out, "inventory.json"), map[string]any{"records": records})
	if err := NativeSync(context.Background(), NativeSyncOptions{Files: []string{files[1], files[0]}, Output: out, Name: "Order", Resume: true, Unsynced: "append", Stage: "cache", MaxResidualMS: 150}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(out, "inventory.json"))
	var actual struct{ Records []MediaRecord }
	json.Unmarshal(data, &actual)
	for i, r := range actual.Records {
		if r.ID != records[i].ID {
			t.Fatal("resume renumbered recording identities")
		}
	}
}
func TestNativePythonIdentityCodec(t *testing.T) {
	got, err := pythonJSON(map[string]any{"unicode": "ع\u2028😀", "offset": 1e-7, "large": 12345678.5, "zero": 0.0, "votes": 6})
	want := "{\"large\": 12345678.5, \"offset\": 1e-07, \"unicode\": \"\\u0639\\u2028\\ud83d\\ude00\", \"votes\": 6, \"zero\": 0.0}"
	if err != nil || string(got) != want {
		t.Fatalf("legacy identity bytes differ: %s (%v)", got, err)
	}
}

func TestNativeLegacyArrayCacheAndDigest(t *testing.T) {
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hashes, err := legacyFingerprints("testdata/legacy-arrays.npz", digest)
	if err != nil || len(hashes) != 2 || hashes[0] != (Fingerprint{1, 2}) || hashes[1] != (Fingerprint{3, 4}) {
		t.Fatalf("legacy fingerprints: %v %v", hashes, err)
	}
	features, err := legacyEnvelope("testdata/legacy-arrays.npz", digest)
	if err != nil || len(features[0]) != 4 || features[2][3] != 11 {
		t.Fatalf("legacy envelope: %v %v", features, err)
	}
	if _, err = legacyFingerprints("testdata/legacy-arrays.npz", "different"); err == nil {
		t.Fatal("stale legacy cache reused")
	}
}
func TestNativeCanonicalUnicodeCompatibility(t *testing.T) {
	data, err := canonicalJSON(map[string]any{"x": "<>&ع\u2028\u2029", "literal": `\u2028`})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"literal\":\"\\\\u2028\",\"x\":\"<>&ع\u2028\u2029\"}"
	if string(data) != want {
		t.Fatalf("canonical unicode changed: %q", data)
	}
}

func TestCompetingGlobalOffsetsPreserveAuditedLocalPair(t *testing.T) {
	records := []MediaRecord{{ID: "anchor-a", Duration: 500}, {ID: "anchor-b", Duration: 400}, {ID: "ambiguous", Duration: 120}, {ID: "local", Duration: 12}}
	profiles := []NativeProfilePair{
		{Reference: 0, Query: 2, Profiles: []*NativeProfile{testProfile(30, "validated_relation"), testProfile(250, "validated_relation")}},
		{Reference: 2, Query: 3, Profiles: []*NativeProfile{testProfile(20, "validated_relation")}},
	}
	got, decisions := selectNativeEdges(records, []*PairEvidence{testRelation(0, 1, 10, 1)}, profiles)
	if len(got) != 2 || got[1].Reference != 2 || got[1].Query != 3 {
		t.Fatalf("local edge not preserved or ambiguous bridge admitted: %+v", decisions)
	}
	_, graph, err := nativeSolve(records, got)
	if err != nil || len(graph["components"].([][]int)) != 2 {
		t.Fatalf("expected two components: %v", err)
	}
}
