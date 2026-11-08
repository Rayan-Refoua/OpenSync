package researchmatch

import (
 "fmt"
 "math"

 "opensync/internal/audio"
)

// WindowSupport describes a distinct acoustic time region examined at an
// existing discovery peak. Multiple frequency bands remain ONE region.
// These windows are NOT held out: discovery searched the same material.
type WindowSupport struct {
 QueryStartSeconds float64 `json:"query_start_seconds"`
 WindowSeconds float64 `json:"window_seconds"`
 AtCandidateScore float64 `json:"at_candidate_score"`
 BestLocalScore float64 `json:"best_local_score"`
 BestLocalOffsetSeconds float64 `json:"best_local_offset_seconds"`
 ResidualMS float64 `json:"residual_ms"`
 BandCorrelations [3]float64 `json:"band_correlations"`
 Status string `json:"status"`
}
type PeakSupport struct {
 DiscoveryRank int `json:"discovery_rank"`
 Candidate Peak `json:"candidate"`
 Status string `json:"status"`
 Windows []WindowSupport `json:"windows"`
 WindowsWithMeasurableSignal int `json:"windows_with_measurable_signal"`
 WindowsWithoutMeasurableSignal int `json:"windows_without_measurable_signal"`
 PositiveLocalWindows int `json:"positive_local_windows"`
}
type SupportReport struct {
 Status string `json:"status"`
 Method string `json:"method"`
 AnalysisProvenance string `json:"analysis_provenance"`
 HopMS float64 `json:"hop_ms"`
 IntegrationMS float64 `json:"integration_ms"`
 PeakCount int `json:"peak_count"`
 Peaks []PeakSupport `json:"peaks"`
 IndependentHoldout string `json:"independent_holdout"`
 GraphCertification string `json:"graph_certification"`
 ExportAuthorization string `json:"export_authorization"`
}

func localPearson(x,y []float64)(float64,bool){
 if len(x)!=len(y)||len(x)<2{return 0,false}
 var sx,sy,sxx,syy,dot float64
 for i,a:=range x{
  b:=y[i]
  sx+=a;sy+=b;sxx+=a*a;syy+=b*b;dot+=a*b
 }
 n:=float64(len(x))
 vx:=sxx-sx*sx/n
 vy:=syy-sy*sy/n
 if vx<=1e-9||vy<=1e-9{return 0,false}
 r:=(dot-sx*sy/n)/math.Sqrt(vx*vy)
 return math.Max(-1,math.Min(1,r)),true
}
func scoreLocal(ref,query [3][]float64,qStart,offset,n int)(float64,[3]float64,bool){
 var corrs [3]float64
 if qStart<0||offset+qStart<0||qStart+n>len(query[0])||offset+qStart+n>len(ref[0]){
  return 0,corrs,false
 }
 for band:=0;band<3;band++{
  v,ok:=localPearson(ref[band][offset+qStart:offset+qStart+n],query[band][qStart:qStart+n])
  if !ok{return 0,corrs,false}
  corrs[band]=v
 }
 var score float64
 for _,v:=range corrs{if v>0{score+=v}}
 return score/3,corrs,true
}
func integerOffset(p Peak,hop int)(int,error){
 if hop<=0{return 0,fmt.Errorf("invalid hop")}
 frames:=math.Round(float64(p.OffsetNS)*SampleRate/(1e9*float64(hop)))
 if math.IsNaN(frames)||math.IsInf(frames,0)||frames>float64(math.MaxInt32)||frames<float64(math.MinInt32){
  return 0,fmt.Errorf("offset outside research frame range")
 }
 lag:=int(frames)
 if math.Abs(float64(p.OffsetNS)-float64(lag*hop)*1e9/SampleRate)>1{
  return 0,fmt.Errorf("discovery offset not aligned with feature hop")
 }
 return lag,nil
}

// AnalyzeSupport computes local residuals for every supplied discovery peak.
// It uses the SAME full-range acoustic data as discovery: multiple windows
// are geographically distinct but NOT independent holdout evidence.
// A short real overlap is divided into two 4-second windows; long overlap
// can have up to twelve 8-second windows without penalizing short signals.
func AnalyzeSupport(ref,query [3][]float64,c Config,peaks []Peak)(SupportReport,error){
 if err:=c.validate();err!=nil{return SupportReport{},err}
 if len(ref[0])==0||len(query[0])==0{return SupportReport{},fmt.Errorf("empty feature arrays")}
 for b:=1;b<3;b++{if len(ref[b])!=len(ref[0])||len(query[b])!=len(query[0]){
  return SupportReport{},fmt.Errorf("feature-band lengths inconsistent")
 }}
 if len(peaks)>c.Peaks{return SupportReport{},fmt.Errorf("too many candidate peaks")}
 report:=SupportReport{
  Status:"ACOUSTIC_WINDOW_SUPPORT_DIAGNOSTIC_NOT_CERTIFIED",
  Method:"same_source_local_three_band_pearson_with_250ms_lag_search_v1",
  AnalysisProvenance:"discovery_source_reused_not_independent",
  HopMS:float64(c.HopSamples)*1000/SampleRate,
  IntegrationMS:float64(c.HopSamples*c.IntegrationFrames)*1000/SampleRate,
  PeakCount:len(peaks),Peaks:[]PeakSupport{},
  IndependentHoldout:"not_performed",GraphCertification:"not_assessed",ExportAuthorization:"denied",
 }
 for rank,p:=range peaks{
  lag,err:=integerOffset(p,c.HopSamples);if err!=nil{return SupportReport{},err}
  qBegin:=0;if lag<0{qBegin=-lag}
  qEnd:=len(query[0]);if v:=len(ref[0])-lag;v<qEnd{qEnd=v}
  available:=qEnd-qBegin
  minFrames:=int(math.Ceil(c.MinOverlapSeconds*SampleRate/float64(c.HopSamples)))
  if available<minFrames{return SupportReport{},fmt.Errorf("candidate outside valid overlap")}
  winSeconds:=8.0
  if available*c.HopSamples<16*SampleRate{winSeconds=4}
  window:=int(math.Round(winSeconds*SampleRate/float64(c.HopSamples)))
  if window>available{window=available}
  count:=available/window
  if count>12{count=12}
  if count<1{count=1}
  item:=PeakSupport{DiscoveryRank:rank+1,Candidate:p,Windows:[]WindowSupport{},Status:"same_source_review_required"}
  search:=int(math.Round(0.250*SampleRate/float64(c.HopSamples)))
  for j:=0;j<count;j++{
   // Deterministic disjoint locations over the entire common time range.
   // Use total free space to distribute windows without overlap.
   qStart:=qBegin
   if count>1{qStart+=int(math.Round(float64(j*(available-window))/float64(count-1)))}
   actual,bc,ok:=scoreLocal(ref,query,qStart,lag,window)
   entry:=WindowSupport{QueryStartSeconds:float64(qStart*c.HopSamples)/SampleRate,
    WindowSeconds:float64(window*c.HopSamples)/SampleRate,AtCandidateScore:actual,
    BestLocalScore:actual,BestLocalOffsetSeconds:float64(lag*c.HopSamples)/SampleRate,
    BandCorrelations:bc,Status:"measured_same_source_not_holdout",
   }
   if !ok{entry.Status="insufficient_acoustic_variance";item.WindowsWithoutMeasurableSignal++}else{
    item.WindowsWithMeasurableSignal++
    // A local lag optimum is only an exploratory measurement, not
    // proof that the same acoustic event was tracked as drift.
    bestLag:=lag
    for candidate:=lag-search;candidate<=lag+search;candidate++{
     score,correlations,valid:=scoreLocal(ref,query,qStart,candidate,window)
     if valid&&score>entry.BestLocalScore{
      entry.BestLocalScore=score;entry.BandCorrelations=correlations
      bestLag=candidate
     }
    }
    entry.BestLocalOffsetSeconds=float64(bestLag*c.HopSamples)/SampleRate
    entry.ResidualMS=float64((bestLag-lag)*c.HopSamples)*1000/SampleRate
    if entry.BestLocalScore>0{item.PositiveLocalWindows++}
   }
   item.Windows=append(item.Windows,entry)
  }
  if item.WindowsWithMeasurableSignal==0{item.Status="all_windows_uninformative"}
  report.Peaks=append(report.Peaks,item)
 }
 return report,nil
}

func MatchWithSupport(ref,query *audio.Reader,c Config)(Result,SupportReport,error){
 a,err:=Extract(ref,c);if err!=nil{return Result{},SupportReport{},err}
 b,err:=Extract(query,c);if err!=nil{return Result{},SupportReport{},err}
 discovery,err:=MatchFeatures(a,b,c);if err!=nil{return Result{},SupportReport{},err}
 support,err:=AnalyzeSupport(a,b,c,discovery.Peaks)
 return discovery,support,err
}
