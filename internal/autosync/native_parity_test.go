package autosync

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"opensync/internal/audio"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeFreshVerificationReference(t *testing.T) {
	source := os.Getenv("OPENSYNC_VERIFY_REFERENCE")
	out := os.Getenv("OPENSYNC_VERIFY_OUTPUT")
	if source == "" || out == "" {
		t.Skip("set explicit reference/output paths for fresh real-audio verification")
	}
	load := func(name string, value any) {
		data, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(data, value); err != nil {
			t.Fatal(err)
		}
	}
	var inventory struct{ Records []MediaRecord }
	var discovery struct{ Pairs []NativePair }
	var expected struct{ Edges []*PairEvidence }
	load("inventory.json", &inventory)
	load("discovery.json", &discovery)
	load("evidence-v2-edges.json", &expected)
	a := NewNativeAnalyzer(inventory.Records)
	defer a.Close()
	got, err := a.verify(context.Background(), discovery.Pairs, out, "evidence-v2")
	if err != nil {
		t.Fatal(err)
	}
	want := map[[2]int]*PairEvidence{}
	for _, e := range expected.Edges {
		want[[2]int{e.Reference, e.Query}] = e
	}
	maxOffset, maxRate := 0., 0.
	if len(got) != len(expected.Edges) {
		t.Errorf("supported pair count %d != %d", len(got), len(expected.Edges))
	}
	for _, e := range got {
		w := want[[2]int{e.Reference, e.Query}]
		if w == nil {
			t.Errorf("unexpected supported pair %d %d", e.Reference, e.Query)
			continue
		}
		offset := math.Abs(e.Selected.Offset - w.Selected.Offset)
		rate := math.Abs(e.Selected.Slope - w.Selected.Slope)
		maxOffset = math.Max(maxOffset, offset)
		maxRate = math.Max(maxRate, rate)
		if offset > 1e-7 || rate > 1e-9 {
			t.Errorf("pair %d %d timing changed: offset %g, rate %g", e.Reference, e.Query, offset, rate)
		}
	}
	if err = writeJSON(filepath.Join(out, "fresh-verification-parity.json"), map[string]any{"pairs": len(discovery.Pairs), "reference_supported": len(expected.Edges), "native_supported": len(got), "maximum_offset_difference_seconds": maxOffset, "maximum_rate_difference": maxRate}); err != nil {
		t.Fatal(err)
	}
}

func TestNativeGraphReferenceDiagnostics(t *testing.T) {
	folder := os.Getenv("OPENSYNC_GRAPH_PARITY_DIR")
	if folder == "" {
		t.Skip("set OPENSYNC_GRAPH_PARITY_DIR for saved evidence parity")
	}
	data, err := os.ReadFile(filepath.Join(folder, "inputs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var input struct {
		Records  []MediaRecord
		Edges    []*PairEvidence
		Profiles []NativeProfilePair
	}
	if err = json.Unmarshal(data, &input); err != nil {
		t.Fatal(err)
	}
	evaluated := []*NativeProfile{}
	for _, p := range input.Profiles {
		for _, h := range p.Profiles {
			copy := *h
			evaluateNativeProfile(&copy)
			evaluated = append(evaluated, &copy)
		}
	}
	if err = writeJSON(filepath.Join(folder, "native-evaluation.json"), evaluated); err != nil {
		t.Fatal(err)
	}
	anchor := nativeAnchorLayout(input.Records, input.Edges)
	selected, decisions := selectNativeEdges(input.Records, input.Edges, input.Profiles)
	layout, graph, err := nativeSolve(input.Records, selected)
	if err != nil {
		t.Fatal(err)
	}
	if err = writeJSON(filepath.Join(folder, "native-graph.json"), map[string]any{"anchors": anchor, "selected": selected, "decisions": decisions, "layout": layout, "graph": graph}); err != nil {
		t.Fatal(err)
	}
}

// Opt-in diagnostics only; no Python process is invoked by tests or runtime.
func TestNativeReferenceDiagnostics(t *testing.T) {
	folder := os.Getenv("OPENSYNC_PARITY_DIR")
	if folder == "" {
		t.Skip("set OPENSYNC_PARITY_DIR for stored reference comparisons")
	}
	data, err := os.ReadFile(filepath.Join(folder, "inputs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var input struct {
		Records []MediaRecord `json:"records"`
		Samples []struct {
			Reference, Query int
			Center, Length   float64
			Prior            NativePrior
		}
		Fingerprints []struct {
			Record        int
			Start, Length float64
		}
	}
	if err = json.Unmarshal(data, &input); err != nil {
		t.Fatal(err)
	}
	a := NewNativeAnalyzer(input.Records)
	defer a.Close()
	var measured []*Measurement
	for _, s := range input.Samples {
		m, err := a.measure(s.Reference, s.Query, s.Center, s.Length, s.Prior)
		if err != nil {
			t.Fatal(err)
		}
		measured = append(measured, m)
	}
	if err = writeJSON(filepath.Join(folder, "native-measurements.json"), measured); err != nil {
		t.Fatal(err)
	}
	for _, s := range input.Fingerprints {
		r, err := audio.Open(input.Records[s.Record].Cache)
		if err != nil {
			t.Fatal(err)
		}
		samples, err := r.ReadFrames(roundFrame(s.Start*16000), roundFrame(s.Length*16000))
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		mag := fingerprintSpectrum(samples)
		data := make([]byte, 4*len(mag))
		for i, v := range mag {
			binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(v))
		}
		if err = os.WriteFile(filepath.Join(folder, "native-spectrum-"+stringValue(s.Start)+".bin"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
