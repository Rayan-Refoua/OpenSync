package autosync

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Opt-in local-media investigation. Persist only evidence, never decoded media
// or reusable analysis caches. The fixture path is supplied by the developer.
func TestShortMediaDiagnostic(t *testing.T) {
	root, report := os.Getenv("OPENSYNC_SHORT_MEDIA"), os.Getenv("OPENSYNC_SHORT_REPORT")
	if root == "" || report == "" {
		t.Skip("set short-media input and report paths")
	}
	files, err := filepath.Glob(filepath.Join(root, "*.wav"))
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	w := &nativeWorkspace{data: map[string][]byte{}, limit: 512 << 20, audio: newDirectAudio()}
	defer w.audio.Close()
	ctx := context.WithValue(context.Background(), workspaceKey{}, w)
	records, err := nativePrepare(ctx, NativeSyncOptions{Output: out}, files, nil)
	if err != nil {
		t.Fatal(err)
	}
	hashes, _, err := NativeFingerprintIndex(ctx, records, out, false)
	if err != nil {
		t.Fatal(err)
	}
	pairs, err := discoverWithNumericalAlternatives(ctx, records, hashes, out)
	if err != nil {
		t.Fatal(err)
	}
	a := NewNativeAnalyzer(records)
	a.ctx = ctx
	defer a.Close()
	edges, err := a.verify(ctx, pairs, out, "evidence-v2")
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := a.recover(ctx, edges, out)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.verify(ctx, recovery, out, "evidence-recovery-v2")
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := a.runProfiles(ctx, out)
	if err != nil {
		t.Fatal(err)
	}
	layout := make([]NativeLayout, len(records))
	for i := range records {
		layout[i] = NativeLayout{Recording: i, Status: "unresolved"}
	}
	distributed, err := runNativeDistributed(ctx, records, layout, recovery, out, a.computeTasks)
	if err != nil {
		t.Fatal(err)
	}
	profiles, err = a.runProfiles(ctx, out)
	if err != nil {
		t.Fatal(err)
	}
	longest := 0
	for i := range records {
		if records[i].Duration > records[longest].Duration {
			longest = i
		}
	}
	waves := []map[string]any{}
	globalWindows := []map[string]any{}
	for q := range records {
		if q == longest {
			continue
		}
		if records[q].Duration >= 2 {
			var halves []*ShortWindowEvidence
			for j := 0; j < 2; j++ {
				block := (records[q].Duration - .1) / 2
				v, e := a.shortGlobalWindow(longest, q, .05+float64(j)*block+.02, block-.04)
				if e != nil {
					t.Fatal(e)
				}
				halves = append(halves, v)
			}
			globalWindows = append(globalWindows, map[string]any{"query": q, "halves": halves})
		}
		m, e := a.waveform(longest, q, .03, 0, records[q].Duration-.06, records[longest].Duration)
		if e != nil {
			t.Fatal(e)
		}
		windows := []*Measurement{}
		if m != nil {
			for j := 0; j < 6; j++ {
				block := (records[q].Duration - .06) / 6
				obs, e := a.measure(longest, q, .03+(float64(j)+.5)*block, block-.02, NativePrior{Offset: m.Offset})
				if e != nil {
					t.Fatal(e)
				}
				windows = append(windows, obs)
			}
		}
		waves = append(waves, map[string]any{"query": q, "whole": m, "windows": windows})
		if m != nil && records[q].Duration >= 2 {
			for _, parts := range []int{2, 3, 4} {
				obs := []*Measurement{}
				block := (records[q].Duration - .1) / float64(parts)
				for j := 0; j < parts; j++ {
					v, e := a.measure(longest, q, .05+(float64(j)+.5)*block, block-.04, NativePrior{Offset: m.Offset})
					if e != nil {
						t.Fatal(e)
					}
					obs = append(obs, v)
				}
				waves = append(waves, map[string]any{"query": q, "parts": parts, "windows": obs})
			}
		}
	}
	evidence := map[string]json.RawMessage{}
	for path, data := range w.data {
		if filepath.Ext(path) == ".json" && (filepath.Base(filepath.Dir(path)) == "evidence-v2" || filepath.Base(filepath.Dir(path)) == "evidence-recovery-v2") {
			evidence[path] = json.RawMessage(data)
		}
	}
	if err = writeJSON(report, map[string]any{"records": records, "discovery": pairs, "recovery": recovery, "evidence": evidence, "profiles": profiles, "distributed": distributed, "waveform": waves, "global_windows": globalWindows}); err != nil {
		t.Fatal(err)
	}
}
