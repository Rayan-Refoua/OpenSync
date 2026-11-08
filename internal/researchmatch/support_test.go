package researchmatch

import (
 "math"
 "math/rand"
 "testing"
)

func patternedFeatures(length int)[3][]float64{
 rng:=rand.New(rand.NewSource(32025))
 var f [3][]float64
 for b:=0;b<3;b++{
  f[b]=make([]float64,length)
  for i:=range f[b]{f[b][i]=rng.NormFloat64()}
 }
 return f
}

func TestShortRealOverlapIsRetainedAsTwoLocalWindows(t *testing.T){
 c:=Dream()
 n:=int(38*SampleRate/c.HopSamples)
 ref:=patternedFeatures(n)
 var query [3][]float64
 for b:=range query{
  query[b]=make([]float64,int(18*SampleRate/c.HopSamples))
  for i:=0;i<int(8*SampleRate/c.HopSamples);i++{
   query[b][i]=ref[b][int(30*SampleRate/c.HopSamples)+i]
  }
 }
 peak:=Peak{OffsetNS:30e9,OffsetSeconds:30,OverlapSeconds:8}
 report,err:=AnalyzeSupport(ref,query,c,[]Peak{peak})
 if err!=nil{t.Fatal(err)}
 if report.ExportAuthorization!="denied"||report.IndependentHoldout!="not_performed"{
  t.Fatal("same-source diagnostics incorrectly certified")
 }
 row:=report.Peaks[0]
 if len(row.Windows)!=2 || row.WindowsWithMeasurableSignal!=2{
  t.Fatalf("8s overlap was discarded: %+v",row)
 }
 for _,win:=range row.Windows{
  if win.WindowSeconds!=4||win.AtCandidateScore<0.99||
   math.Abs(win.ResidualMS)>1e-6{
   t.Fatalf("short genuine window not measured correctly: %+v",win)
  }
 }
}
func TestLongOverlapUsesDistinctDisjointWindowsAndFindsLag(t *testing.T){
 c:=Dream()
 const seconds=90
 f:=patternedFeatures(seconds*SampleRate/c.HopSamples)
 lag:=1000
 var q [3][]float64
 for b:=0;b<3;b++{
  q[b]=append([]float64(nil),f[b][lag:lag+int(70*SampleRate/c.HopSamples)]...)
 }
 peak:=Peak{OffsetNS:int64(lag*c.HopSamples)*1e9/SampleRate}
 r,err:=AnalyzeSupport(f,q,c,[]Peak{peak})
 if err!=nil{t.Fatal(err)}
 row:=r.Peaks[0]
 if len(row.Windows)!=8||row.WindowsWithMeasurableSignal!=8{
  t.Fatalf("unexpected local windows: %+v",row)
 }
 previousEnd:=0.0
 for i,win:=range row.Windows{
  if i>0&&win.QueryStartSeconds<previousEnd-1e-6{
   t.Fatal("overlapping local windows falsely treated as separate")
  }
  if win.AtCandidateScore<0.99||math.Abs(win.ResidualMS)>1e-6{
   t.Fatalf("correct measured window wrong %+v",win)
  }
  previousEnd=win.QueryStartSeconds+win.WindowSeconds
 }
}
func TestMisalignedCandidateIsRejected(t *testing.T){
 c:=Dream()
 f:=patternedFeatures(6000)
 _,err:=AnalyzeSupport(f,f,c,[]Peak{{OffsetNS:1}})
 if err==nil{t.Fatal("non-grid research offset should not be silently rounded")}
}
func TestSilentIntervalsAreExplicitlyUninformative(t *testing.T){
 c:=Dream()
 var a,b [3][]float64
 for band:=0;band<3;band++{
  a[band]=make([]float64,6000)
  b[band]=make([]float64,6000)
 }
 r,err:=AnalyzeSupport(a,b,c,[]Peak{{OffsetNS:0}})
 if err!=nil{t.Fatal(err)}
 if r.Peaks[0].WindowsWithMeasurableSignal!=0||
  r.Peaks[0].Status!="all_windows_uninformative"{
  t.Fatal("silence described as corroborating")
 }
}
