package autosync

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func solutionModel(t *testing.T, p *NativeProject, recording int) TimeModel {
	t.Helper()
	for _, m := range p.Data.Models {
		if m.Recording == recording {
			return m
		}
	}
	t.Fatalf("missing recording %d", recording)
	return TimeModel{}
}

func solutionFixture(t *testing.T) (string, []MediaRecord) {
	t.Helper()
	r, _, _ := chronologyFixture()
	folder := t.TempDir()
	for i := range r {
		r[i].Source = filepath.Join(folder, r[i].Name)
		if err := os.WriteFile(r[i].Source, nil, 0600); err != nil {
			t.Fatal(err)
		}
		r[i].Duration = 60
		r[i].Video["duration"] = "60"
		r[i].Video["nb_frames"] = "3000"
	}
	edges := []*PairEvidence{testRelation(0, 1, 12, 1), testRelation(0, 2, 24, 1), testRelation(2, 3, 16, 1)}
	layout, graph, err := nativeSolve(r, edges)
	if err != nil {
		t.Fatal(err)
	}
	graph["placement_mode"] = "clips"
	path := filepath.Join(t.TempDir(), "OpenSync.opensync")
	if err = saveNativeProject(path, "review fixture", r, edges, layout, graph); err != nil {
		t.Fatal(err)
	}
	return path, r
}

func TestSolutionRevisionPreservesEvidenceAndCorrections(t *testing.T) {
	base, r := solutionFixture(t)
	original, _ := os.ReadFile(base)
	revise := func(path string, c ReviewChange) string {
		t.Helper()
		next, e := CreateSolution(path, c)
		if e != nil {
			t.Fatal(e)
		}
		return next
	}
	load := func(path string) *NativeProject {
		t.Helper()
		p, e := LoadNativeProject(path)
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	rejected := revise(base, ReviewChange{Kind: "reject_recording", Recording: r[1].ID})
	p := load(rejected)
	for _, key := range []string{"active_edges", "global_evidence_residuals"} {
		values, _ := p.Data.Graph[key].([]any)
		for _, v := range values {
			if key == "global_evidence_residuals" {
				v = v.(map[string]any)["edge"]
			}
			if number(v) == 0 {
				t.Fatal("review diagnostics refer to removed edge", key)
			}
		}
	}
	if solutionModel(t, p, 1).Resolved || !strings.HasPrefix(solutionModel(t, p, 1).Status, "unresolved") {
		t.Fatal("rejected recording still placed", solutionModel(t, p, 1))
	}
	rebuilt := revise(rejected, ReviewChange{Kind: "rebuild"})
	chronological := revise(rebuilt, ReviewChange{Kind: "mode", Mode: "chronological"})
	if solutionModel(t, load(chronological), 1).Resolved {
		t.Fatal("chronology resurrected manual rejection")
	}
	ledger, err := correctionsForRecords(chronological, r)
	if err != nil || !reflect.DeepEqual(ledger.Recordings, []string{r[1].ID}) {
		t.Fatal(ledger, err)
	}
	// Fresh-run finalization receives new evidence but must honor saved corrections.
	newOutput := t.TempDir()
	edges := []*PairEvidence{testRelation(0, 1, 12, 1), testRelation(0, 2, 24, 1), testRelation(2, 3, 16, 1)}
	profiles := []NativeProfilePair{{Reference: 0, Query: 1, Profiles: []*NativeProfile{testProfile(12, "validated_relation")}}}
	if err = nativeFinish(r, edges, profiles, nil, NativeSyncOptions{Output: newOutput, Name: "corrected fresh sync", Mode: "chronological", Corrections: chronological, Unsynced: "append", MaxResidualMS: 150}); err != nil {
		t.Fatal(err)
	}
	if solutionModel(t, load(filepath.Join(newOutput, "OpenSync.opensync")), 1).Resolved {
		t.Fatal("fresh finalization lost rejection")
	}
	changed := append([]MediaRecord(nil), r...)
	changed[1].SourceBytes++
	if _, err = correctionsForRecords(chronological, changed); err == nil {
		t.Fatal("changed source accepted correction")
	}
	if _, err = correctionsForRecords(chronological, r[2:]); err == nil {
		t.Fatal("missing rejected source accepted")
	}
	restored := revise(rebuilt, ReviewChange{Kind: "restore_recording", Recording: r[1].ID})
	if !solutionModel(t, load(restored), 1).Resolved {
		t.Fatal("restored placement not recovered")
	}
	pair := revise(base, ReviewChange{Kind: "reject_pair", Reference: r[3].ID, Query: r[2].ID})
	if solutionModel(t, load(pair), 3).Resolved {
		t.Fatal("rejected bridge still placed")
	}
	pairRebuild := revise(pair, ReviewChange{Kind: "rebuild"})
	if solutionModel(t, load(pairRebuild), 3).Resolved {
		t.Fatal("rebuild lost pair rejection")
	}
	pairRestore := revise(pairRebuild, ReviewChange{Kind: "restore_pair", Reference: r[2].ID, Query: r[3].ID})
	if !solutionModel(t, load(pairRestore), 3).Resolved {
		t.Fatal("pair restore did not reconnect")
	}
	parent := load(restored).Data.Graph["solution_revision"].(map[string]any)["parent_project"].(string)
	if filepath.Clean(parent) != filepath.Clean(rebuilt) || solutionModel(t, load(parent), 1).Resolved {
		t.Fatal("undo parent is incorrect")
	}
	after, _ := os.ReadFile(base)
	if !bytes.Equal(original, after) {
		t.Fatal("original project modified")
	}
	baseRaw, _ := rawObject(load(base).Payload)
	revisedRaw, _ := rawObject(p.Payload)
	if !reflect.DeepEqual(baseRaw["validated_relations"], revisedRaw["validated_relations"]) {
		t.Fatal("acoustic evidence altered")
	}
	delta, _ := os.ReadFile(rejected)
	if bytes.Contains(delta, []byte("validated_relations")) || bytes.Contains(delta, []byte("\"recordings\"")) {
		t.Fatal("revision duplicated evidence or inventory")
	}
	if err = ExportNative(rejected, NativeExportOptions{Output: filepath.Join(t.TempDir(), "export"), Unsynced: "append", MaxResidualMS: 150}); err != nil {
		t.Fatal(err)
	}
}

func TestSolutionRevisionIntegrity(t *testing.T) {
	base, _ := solutionFixture(t)
	revision, err := CreateSolution(base, ReviewChange{Kind: "rebuild"})
	if err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(revision)
	var wrapper NativeProject
	if err = json.Unmarshal(original, &wrapper); err != nil {
		t.Fatal(err)
	}
	wrapper.Hash = strings.Repeat("0", 64)
	if err = writeJSON(revision, wrapper); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadNativeProject(revision); err == nil {
		t.Fatal("modified revision checksum accepted")
	}
	if err = os.WriteFile(revision, original, 0600); err != nil {
		t.Fatal(err)
	}
	baseData, _ := os.ReadFile(base)
	raw, _ := rawObject(baseData)
	payload := raw["payload"].(map[string]any)
	payload["name"] = "changed base"
	canonical, err := canonicalJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw["content_sha256"] = shaBytes(canonical)
	if err = writeJSON(base, raw); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadNativeProject(revision); err == nil || !strings.Contains(err.Error(), "base project was changed") {
		t.Fatal("base substitution not detected", err)
	}
}
