package premierexml

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"opensync/internal/projectstate"
)

// ApprovedPlacement is an externally reviewed position. The current OpenSync
// pipeline has no independent certification producer. This input is never
// interpreted as proof of acoustic synchronization.
type ApprovedPlacement struct {
	RecordingID string `json:"recording_id"`
	ExpectedStartFrame int64 `json:"expected_start_frame"`
	NewStartFrame int64 `json:"new_start_frame"`
	EvidenceReference string `json:"evidence_reference"`
	Approval string `json:"approval"`
}

type FallbackRequest struct {
	Version int `json:"version"`
	Placements []ApprovedPlacement `json:"placements"`
	AllowedLostOverlapPairs []BaselinePair `json:"allowed_lost_overlap_pairs"`
}

type FallbackRecording struct {
	RecordingID string `json:"recording_id"`
	SourcePath string `json:"source_path"`
	OriginalStartFrame int64 `json:"original_start_frame"`
	OutputStartFrame int64 `json:"output_start_frame"`
	Status string `json:"status"`
	EvidenceReference string `json:"evidence_reference,omitempty"`
}

type FallbackManifest struct {
	Version int `json:"version"`
	Status string `json:"status"`
	Recordings int `json:"recordings"`
	InheritedPlacements int `json:"inherited_placements"`
	ExternallyApprovedPlacements int `json:"externally_approved_placements"`
	AcousticCertification string `json:"acoustic_certification"`
	Coverage BaselineCoverage `json:"coverage"`
	Placements []FallbackRecording `json:"placements"`
}

func normalizedBaselineName(name string) string {
	return strings.ToLower(path.Base(strings.ReplaceAll(name,"\\","/")))
}

// ExportBaselineFallback retains every source's PluralEyes placement unless
// an explicit external review supplies a replacement. Replacements require
// an exact original-position guard, linked-audio preservation and an explicit
// whitelist for lost timeline overlaps. No input is considered self-certifying.
func ExportBaselineFallback(source []byte,p *projectstate.Project,request FallbackRequest)([]byte,FallbackManifest,error){
	if request.Version!=1{return nil,FallbackManifest{},fmt.Errorf("unsupported fallback request version")}
	if err:=p.Validate();err!=nil{return nil,FallbackManifest{},err}
	slots,err:=parseSlots(source)
	if err!=nil{return nil,FallbackManifest{},err}
	byName:=make(map[string]string,len(p.Recordings))
	for id,r:=range p.Recordings{
		name:=normalizedBaselineName(r.SourcePath)
		if _,ok:=byName[name];ok{return nil,FallbackManifest{},fmt.Errorf("ambiguous source basename %q",name)}
		byName[name]=id
	}
	_,videos,err:=baselinePairs(slots,byName)
	if err!=nil{return nil,FallbackManifest{},err}
	if len(videos)!=len(p.Recordings){return nil,FallbackManifest{},fmt.Errorf("baseline does not include every recording")}
	audio:=make(map[string][]slot)
	for _,s:=range slots {
		if s.Media!="audio"{continue}
		id,ok:=byName[normalizedBaselineName(s.Name)]
		if !ok{continue}
		video:=videos[id]
		if s.Start!=video.Start||s.End!=video.End||s.In!=video.In||s.Out!=video.Out{
			return nil,FallbackManifest{},fmt.Errorf("baseline audio not aligned with its video for %s",id)
		}
		audio[id]=append(audio[id],s)
	}
	for id:=range videos{
		if len(audio[id])==0{return nil,FallbackManifest{},fmt.Errorf("missing linked baseline audio for %s",id)}
	}
	approved:=make(map[string]ApprovedPlacement)
	updates:=make([]Placement,0,3*len(request.Placements))
	for _,item:=range request.Placements{
		v,ok:=videos[item.RecordingID]
		if !ok{return nil,FallbackManifest{},fmt.Errorf("unknown recording %q",item.RecordingID)}
		if _,exists:=approved[item.RecordingID];exists{return nil,FallbackManifest{},fmt.Errorf("duplicate approval %q",item.RecordingID)}
		if item.Approval!="externally_reviewed"||strings.TrimSpace(item.EvidenceReference)==""{
			return nil,FallbackManifest{},fmt.Errorf("missing external review/evidence for %s",item.RecordingID)
		}
		if item.ExpectedStartFrame!=v.Start||item.NewStartFrame<0{
			return nil,FallbackManifest{},fmt.Errorf("stale or invalid placement for %s",item.RecordingID)
		}
		if item.NewStartFrame==v.Start{
			return nil,FallbackManifest{},fmt.Errorf("unchanged recording should inherit baseline %s",item.RecordingID)
		}
		approved[item.RecordingID]=item
		linked:=append([]slot{v},audio[item.RecordingID]...)
		for _,s:=range linked{
			updates=append(updates,Placement{
				ClipID:s.ID,Media:s.Media,Track:s.Track,
				ExpectedStart:s.Start,ExpectedEnd:s.End,NewStart:item.NewStartFrame,
			})
		}
	}
	result:=append([]byte(nil),source...)
	if len(updates)>0{
		result,err=Patch(source,Plan{Placements:updates})
		if err!=nil{return nil,FallbackManifest{},err}
	}
	coverage,err:=CompareBaselineCoverage(source,result,p)
	if err!=nil{return nil,FallbackManifest{},err}
	allowed:=make(map[BaselinePair]bool)
	for _,pair:=range request.AllowedLostOverlapPairs{
		if pair.First==""||pair.Second==""||pair.First>=pair.Second{return nil,FallbackManifest{},fmt.Errorf("invalid overlap-loss authorization")}
		if allowed[pair]{return nil,FallbackManifest{},fmt.Errorf("duplicate overlap-loss authorization")}
		allowed[pair]=true
	}
	for _,pair:=range coverage.LostOverlapPairs {
		if !allowed[pair]{return nil,FallbackManifest{},fmt.Errorf("unapproved loss of PluralEyes overlap %s/%s",pair.First,pair.Second)}
		delete(allowed,pair)
	}
	if len(allowed)>0{return nil,FallbackManifest{},fmt.Errorf("unused overlap-loss authorizations")}
	ids:=make([]string,0,len(videos))
	for id:=range videos{ids=append(ids,id)}
	sort.Strings(ids)
	manifest:=FallbackManifest{
		Version:1,Status:"PLURALEYES_FALLBACK_WITH_EXTERNAL_REVIEW",
		Recordings:len(videos),InheritedPlacements:len(videos)-len(approved),
		ExternallyApprovedPlacements:len(approved),
		AcousticCertification:"not_independently_assessed",
		Coverage:coverage,Placements:make([]FallbackRecording,0,len(ids)),
	}
	for _,id:=range ids {
		v:=videos[id]
		row:=FallbackRecording{
			RecordingID:id,SourcePath:p.Recordings[id].SourcePath,
			OriginalStartFrame:v.Start,OutputStartFrame:v.Start,
			Status:"pluraleyes_baseline_unverified",
		}
		if item,ok:=approved[id];ok{
			row.OutputStartFrame=item.NewStartFrame
			row.Status="externally_reviewed_not_independently_certified"
			row.EvidenceReference=item.EvidenceReference
		}
		manifest.Placements=append(manifest.Placements,row)
	}
	return result,manifest,nil
}
