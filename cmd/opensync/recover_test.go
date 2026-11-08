package main

import (
	"encoding/json"
	"testing"

	"opensync/internal/projectstate"
	"opensync/internal/quality"
)

func TestRecoveryCandidateSelectionAlternatives(t *testing.T) {
	p:=projectstate.New()
	for _,r:=range []projectstate.Recording{
		{ID:"a",SourcePath:`C:\videos\C0594.MP4`,DurationNS:10_000_000_000},
		{ID:"b",SourcePath:"/video/2148.MP4",DurationNS:20_000_000_000},
		{ID:"c",SourcePath:"/video/2160.MP4",DurationNS:30_000_000_000},
	}{
		if err:=p.AddRecording(r);err!=nil{t.Fatal(err)}
	}
	for _,h:=range []projectstate.Hypothesis{
		{ID:"first",Reference:"b",Query:"a",OffsetNS:1,Status:projectstate.Discovered},
		{ID:"second",Reference:"c",Query:"a",OffsetNS:2,Status:projectstate.Discovered},
		{ID:"rejected",Reference:"c",Query:"a",OffsetNS:3,Status:projectstate.Rejected},
	}{
		if err:=p.AddHypothesis(h);err!=nil{t.Fatal(err)}
	}
	got,err:=recoveryCandidates(p,"C0594.MP4")
	if err!=nil{t.Fatal(err)}
	if len(got)!=2||got[0].ID!="first"||got[1].ID!="second" {t.Fatalf("alternatives lost: %+v",got)}
	if _,err:=recoveryCandidates(p,"nonexistent.mp4");err==nil {t.Fatal("missing recording accepted")}
	if recoveryLimit(quality.Draft)>=recoveryLimit(quality.Normal) || recoveryLimit(quality.Normal)>=recoveryLimit(quality.RockSolid) {
		t.Fatal("presets do not increase investigation depth")
	}
}

func TestRecoveryRejectsControlDominatedCandidate(t *testing.T) {
	windows:=make([]any,10)
	for i:=range windows{
		windows[i]=map[string]any{
			"measurement":map[string]any{"measured_offset_ns":1790998000000+int64(i)*10000000,"correlation":0.02},
			"wrong_offset_controls":[]any{
				map[string]any{"correlation":0.08},
				map[string]any{"correlation":0.04},
			},
		}
	}
	data,err:=json.Marshal(map[string]any{"probes":windows})
	if err!=nil{t.Fatal(err)}
	var e RecoveryEntry
	if err:=evidenceRecovery(data,&e);err!=nil{t.Fatal(err)}
	if e.Outcome!="control_dominated"||e.ControlWins!=10||e.Windows!=10||e.OffsetSpreadMS!=90{
		t.Fatalf("bad evidence unexpectedly accepted: %+v",e)
	}
}

func TestRecoveryPreservesCandidateWithUsefulControls(t *testing.T) {
	windows:=make([]any,10)
	for i:=range windows{
		windows[i]=map[string]any{
			"measurement":map[string]any{"measured_offset_ns":52711000000+int64(i)*1000000,"correlation":0.24},
			"wrong_offset_controls":[]any{map[string]any{"correlation":0.02}},
		}
	}
	data,err:=json.Marshal(map[string]any{"probes":windows})
	if err!=nil{t.Fatal(err)}
	var e RecoveryEntry
	if err:=evidenceRecovery(data,&e);err!=nil{t.Fatal(err)}
	if e.Outcome!="candidate_needs_holdout"||e.ControlWins!=0 {t.Fatalf("candidate incorrectly rejected: %+v",e)}
}
