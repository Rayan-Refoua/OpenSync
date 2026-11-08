package premierexml

import (
	"bytes"
	"strings"
	"testing"
)

func TestBaselineFallbackPreservesAllWhenUnresolved(t *testing.T) {
	source:=completePreviewFixture()
	output,manifest,err:=ExportBaselineFallback(source,previewProject(t),FallbackRequest{Version:1})
	if err!=nil{t.Fatal(err)}
	if !bytes.Equal(source,output){t.Fatal("baseline bytes changed without approvals")}
	if manifest.Recordings!=3||manifest.InheritedPlacements!=3||
		manifest.ExternallyApprovedPlacements!=0||
		manifest.AcousticCertification!="not_independently_assessed"{
		t.Fatalf("baseline misrepresented: %+v",manifest)
	}
	for _,p:=range manifest.Placements{
		if p.Status!="pluraleyes_baseline_unverified"||p.OriginalStartFrame!=p.OutputStartFrame{
			t.Fatalf("unresolved source changed: %+v",p)
		}
	}
}

func TestBaselineFallbackReviewedPlacementRetainsOtherSources(t *testing.T){
	source:=completePreviewFixture()
	request:=FallbackRequest{Version:1,Placements:[]ApprovedPlacement{{
		RecordingID:"B-id",ExpectedStartFrame:10,NewStartFrame:130,
		EvidenceReference:"human-reviewed-check-001",Approval:"externally_reviewed",
	}}}
	output,manifest,err:=ExportBaselineFallback(source,previewProject(t),request)
	if err!=nil{t.Fatal(err)}
	if bytes.Equal(source,output){t.Fatal("approved placement not applied")}
	if manifest.InheritedPlacements!=2||manifest.ExternallyApprovedPlacements!=1{
		t.Fatalf("unexpected fallback accounting: %+v",manifest)
	}
	if manifest.Coverage.ChangedVideoPlacements!=1||len(manifest.Coverage.LostOverlapPairs)!=0{
		t.Fatalf("coverage regression: %+v",manifest.Coverage)
	}
	if !bytes.Contains(output,[]byte(`<clipitem id="target-a"><name>B</name><start>130</start>`)){
		t.Fatal("audio failed to move with reviewed video")
	}
	seen:=0
	for _,p:=range manifest.Placements{
		if p.RecordingID=="B-id"{
			seen++
			if p.Status!="externally_reviewed_not_independently_certified"||
				p.EvidenceReference!="human-reviewed-check-001"{t.Fatalf("falsely certified: %+v",p)}
		}else if p.Status!="pluraleyes_baseline_unverified"||
			p.OriginalStartFrame!=p.OutputStartFrame{
			t.Fatalf("unreviewed recording moved: %+v",p)
		}
	}
	if seen!=1{t.Fatal("missing reviewed recording")}
}

func TestBaselineFallbackRequiresExactOverlapLossAuthorization(t *testing.T){
	source:=completePreviewFixture()
	source=bytes.ReplaceAll(source,
		[]byte("<start>10</start><end>30</end>"),
		[]byte("<start>170</start><end>190</end>"))
	request:=FallbackRequest{Version:1,Placements:[]ApprovedPlacement{{
		RecordingID:"B-id",ExpectedStartFrame:170,NewStartFrame:10,
		EvidenceReference:"human-reviewed-check-002",Approval:"externally_reviewed",
	}}}
	if _,_,err:=ExportBaselineFallback(source,previewProject(t),request);err==nil{
		t.Fatal("accepted loss of baseline overlap without approval")
	}
	request.AllowedLostOverlapPairs=[]BaselinePair{{First:"A-id",Second:"B-id"}}
	_,manifest,err:=ExportBaselineFallback(source,previewProject(t),request)
	if err!=nil{t.Fatal(err)}
	if len(manifest.Coverage.LostOverlapPairs)!=1{
		t.Fatalf("missing intentional overlap loss: %+v",manifest.Coverage)
	}
	request.AllowedLostOverlapPairs=append(request.AllowedLostOverlapPairs,BaselinePair{First:"A-id",Second:"C-id"})
	if _,_,err:=ExportBaselineFallback(source,previewProject(t),request);err==nil{
		t.Fatal("accepted unused overlap-loss approval")
	}
}

func TestBaselineFallbackRejectsUnreviewedOrInvalidPlacement(t *testing.T){
	source:=completePreviewFixture()
	for _,item:=range []ApprovedPlacement{
		{RecordingID:"B-id",ExpectedStartFrame:10,NewStartFrame:130,Approval:"independently_verified",EvidenceReference:"claim"},
		{RecordingID:"B-id",ExpectedStartFrame:11,NewStartFrame:130,Approval:"externally_reviewed",EvidenceReference:"proof"},
		{RecordingID:"B-id",ExpectedStartFrame:10,NewStartFrame:130,Approval:"externally_reviewed"},
		{RecordingID:"B-id",ExpectedStartFrame:10,NewStartFrame:10,Approval:"externally_reviewed",EvidenceReference:"proof"},
		{RecordingID:"missing",ExpectedStartFrame:10,NewStartFrame:130,Approval:"externally_reviewed",EvidenceReference:"proof"},
	}{
		if _,_,err:=ExportBaselineFallback(source,previewProject(t),FallbackRequest{Version:1,Placements:[]ApprovedPlacement{item}});err==nil{
			t.Fatalf("invalid placement accepted: %+v",item)
		}
	}
	broken:=[]byte(strings.Replace(string(source),`<clipitem id="target-a"><name>B</name><start>10</start>`,`<clipitem id="target-a"><name>B</name><start>11</start>`,1))
	if _,_,err:=ExportBaselineFallback(broken,previewProject(t),FallbackRequest{Version:1});err==nil{
		t.Fatal("accepted misaligned baseline audio")
	}
}
