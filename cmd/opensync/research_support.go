package main

import (
 "encoding/json"
 "fmt"
 "math"
 "os"
 "strings"

 "opensync/internal/audio"
 "opensync/internal/projectstate"
 "opensync/internal/researchmatch"
)

type supportFile struct {
 Status string `json:"status"`
 Reference string `json:"reference"`
 Query string `json:"query"`
 SourceDiscoveryReport string `json:"source_discovery_report"`
 Report researchmatch.SupportReport `json:"report"`
}
func researchSupportCommand(projectPath,indexPath,discoveryPath,output string)error{
 for _,p:=range []string{projectPath,indexPath,discoveryPath}{
  if output==p{return fmt.Errorf("output must not overwrite input")}
 }
 project,err:=projectstate.Load(projectPath);if err!=nil{return err}
 data,err:=os.ReadFile(discoveryPath);if err!=nil{return err}
 var discovery researchReport
 if err:=json.Unmarshal(data,&discovery);err!=nil{return err}
 if discovery.Status!="RESEARCH_DISCOVERY_ONLY_NOT_VERIFIED"||discovery.Research.Certification!="not_assessed"{
  return fmt.Errorf("unsupported discovery report provenance")
 }
 refRec,refOK:=project.Recordings[discovery.Reference]
 queryRec,qOK:=project.Recordings[discovery.Query]
 if !refOK||!qOK||discovery.Reference==discovery.Query{return fmt.Errorf("unknown or identical recording identities")}
 norm:=func(s string)string{return strings.ToLower(strings.ReplaceAll(s,"\\","/"))}
 if norm(refRec.SourcePath)!=norm(discovery.ReferenceSource)||norm(queryRec.SourcePath)!=norm(discovery.QuerySource){
  return fmt.Errorf("discovery report recording identity mismatch")
 }
 var cache map[string]probeCacheEntry
 raw,err:=os.ReadFile(indexPath);if err!=nil{return err}
 if err:=json.Unmarshal(raw,&cache);err!=nil{return err}
 open:=func(id,source string)(*audio.Reader,error){
  item,ok:=cache[id]
  if !ok||item.Cache==""||norm(item.Source)!=norm(source){
   return nil,fmt.Errorf("cache identity mismatch for %q",id)
  }
  return audio.Open(item.Cache)
 }
 ref,err:=open(discovery.Reference,refRec.SourcePath);if err!=nil{return err}
 defer ref.Close()
 query,err:=open(discovery.Query,queryRec.SourcePath);if err!=nil{return err}
 defer query.Close()
 cfg:=researchmatch.Dream()
 if math.Abs(discovery.Research.HopMS-8)<1e-9&&math.Abs(discovery.Research.IntegrationMS-8)<1e-9{
  cfg.HopSamples=128
 }else if math.Abs(discovery.Research.HopMS-2)<1e-9{
  cfg.HopSamples=32
  if math.Abs(discovery.Research.IntegrationMS-8)<1e-9{cfg.IntegrationFrames=4
  }else if math.Abs(discovery.Research.IntegrationMS-2)>1e-9{
   return fmt.Errorf("unknown 2ms integration scheme")
  }
 }else{return fmt.Errorf("unsupported feature resolution and integration")}
 if len(discovery.Research.Peaks)>cfg.Peaks{return fmt.Errorf("too many reported discovery peaks")}
 if discovery.Research.Method!="three_band_log_energy_derivative_fft_pearson_v1"{
  return fmt.Errorf("unexpected source feature extraction method")
 }
 a,err:=researchmatch.Extract(ref,cfg);if err!=nil{return err}
 b,err:=researchmatch.Extract(query,cfg);if err!=nil{return err}
 // The report is an untrusted candidate list; all comparisons are
 // recomputed directly against the immutable source cache.
 support,err:=researchmatch.AnalyzeSupport(a,b,cfg,discovery.Research.Peaks)
 if err!=nil{return err}
 file:=supportFile{
  Status:"RESEARCH_SUPPORT_DIAGNOSTIC_NOT_CERTIFIED",
  Reference:discovery.Reference,Query:discovery.Query,
  SourceDiscoveryReport:discoveryPath,Report:support,
 }
 bytes,err:=json.MarshalIndent(file,"","  ");if err!=nil{return err}
 bytes=append(bytes,'\n')
 if err:=exclusiveCreatePreview(output,bytes);err!=nil{return err}
 fmt.Printf("STATUS: %s\nPEAKS: %d\nINDEPENDENT_HOLDOUT: %s\nCERTIFICATION: %s\nREPORT: %s\n",
  file.Status,len(support.Peaks),support.IndependentHoldout,support.GraphCertification,output)
 for _,p:=range support.Peaks{
  fmt.Printf("PEAK_%02d OFFSET=%+.3fs OVERLAP=%.2fs WINDOWS=%d INFORMATIVE=%d LOCAL_POSITIVE=%d\n",
   p.DiscoveryRank,p.Candidate.OffsetSeconds,p.Candidate.OverlapSeconds,
   len(p.Windows),p.WindowsWithMeasurableSignal,p.PositiveLocalWindows)
 }
 return nil
}
