package main

import (
 "encoding/json"
 "fmt"
 "os"
 "sort"

 "opensync/internal/projectstate"
 "opensync/internal/sourcetracks"
)

type SequentialReview struct {
 Status string `json:"status"`
 NeighborPairs int `json:"neighbor_pairs"`
 WithDiscovery int `json:"with_discovery"`
 MissingDiscovery int `json:"missing_discovery"`
 Pairs []SequentialPair `json:"pairs"`
}

type SequentialPair struct {
 Before string `json:"before"`
 After string `json:"after"`
 BeforeSource string `json:"before_source"`
 AfterSource string `json:"after_source"`
 Basis string `json:"basis"`
 Continuity string `json:"continuity"`
 Hypotheses []string `json:"discovery_hypotheses"`
 Status string `json:"status"`
}

func sequencePlanProject(p *projectstate.Project)(SequentialReview,error){
 neighbors,err:=sourcetracks.SequentialNeighbors(p)
 if err!=nil{return SequentialReview{},err}
 result:=SequentialReview{Status:"CHRONOLOGY_PROPOSED_NOT_VERIFIED",NeighborPairs:len(neighbors),Pairs:make([]SequentialPair,0,len(neighbors))}
 for _,n:=range neighbors{
  row:=SequentialPair{
   Before:n.Before,After:n.After,
   BeforeSource:p.Recordings[n.Before].SourcePath,
   AfterSource:p.Recordings[n.After].SourcePath,
   Basis:n.Basis,Continuity:n.Continuity,Hypotheses:[]string{},
   Status:"discovery_not_found",
  }
  for _,h:=range p.Hypotheses{
   if h.Status==projectstate.Rejected{continue}
   if h.Reference==n.Before&&h.Query==n.After || h.Reference==n.After&&h.Query==n.Before {
    row.Hypotheses=append(row.Hypotheses,h.ID)
   }
  }
  sort.Strings(row.Hypotheses)
  if len(row.Hypotheses)>0{
   row.Status="discovery_available_requires_acoustic_validation"
   result.WithDiscovery++
  }else{result.MissingDiscovery++}
  result.Pairs=append(result.Pairs,row)
 }
 return result,nil
}

func sequencePlanCommand(projectPath,outputPath string)error{
 p,err:=projectstate.Load(projectPath)
 if err!=nil{return err}
 plan,err:=sequencePlanProject(p)
 if err!=nil{return err}
 data,err:=json.MarshalIndent(plan,"","  ")
 if err!=nil{return err}
 data=append(data,'\n')
 f,err:=os.OpenFile(outputPath,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
 if err!=nil{return err}
 if _,err=f.Write(data);err!=nil{f.Close();os.Remove(outputPath);return err}
 if err=f.Close();err!=nil{os.Remove(outputPath);return err}
 fmt.Printf("STATUS: %s\nSEQUENTIAL_PAIRS: %d\nDISCOVERY_AVAILABLE: %d\nDISCOVERY_MISSING: %d\nOUTPUT: %s\n",plan.Status,plan.NeighborPairs,plan.WithDiscovery,plan.MissingDiscovery,outputPath)
 return nil
}
