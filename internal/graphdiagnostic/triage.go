package graphdiagnostic

import (
 "fmt"
 "math"
 "sort"

 "opensync/internal/projectstate"
)

type TriagePair struct {
 First string `json:"first"`
 Second string `json:"second"`
 RepresentativeID string `json:"representative_id"`
 AlternateHypothesisIDs []string `json:"alternate_hypothesis_ids"`
 DistinctObservationEvents int `json:"distinct_observation_events"`
 DiscoverySupportWindows int `json:"discovery_support_windows"`
 Paths []TriagePath `json:"two_hop_paths"`
}
type TriagePath struct {
 Via string `json:"via"`
 FirstHypothesisID string `json:"first_hypothesis_id"`
 SecondHypothesisID string `json:"second_hypothesis_id"`
 PredictedOffsetNS int64 `json:"predicted_offset_ns"`
 ResidualNS int64 `json:"residual_ns"`
}
type TriageReport struct {
 Status string `json:"status"`
 Recordings int `json:"recordings"`
 InputCandidates int `json:"input_candidates"`
 PairGroups int `json:"pair_groups"`
 Representatives int `json:"representatives"`
 AlternativeHypothesesRetained int `json:"alternative_hypotheses_retained"`
 TwoHopPathCount int `json:"two_hop_path_count"`
 PairsWithoutPath int `json:"pairs_without_path"`
 ConnectedComponents [][]string `json:"connected_components"`
 RepresentativePolicy string `json:"representative_policy"`
 Certification string `json:"certification"`
 Pairs []TriagePair `json:"pairs"`
}
type pairKey struct{first,second string}
func canonical(a,b string)pairKey{
 if a>b {a,b=b,a}
 return pairKey{a,b}
}
func addInt(a,b int64)(int64,error){
 if b>0&&a>math.MaxInt64-b||b<0&&a<math.MinInt64-b{
  return 0,fmt.Errorf("offset addition overflow")
 }
 return a+b,nil
}
func directedOffset(h projectstate.Hypothesis,from,to string)(int64,error){
 if h.Reference==from&&h.Query==to{return h.OffsetNS,nil}
 if h.Query==from&&h.Reference==to{
  if h.OffsetNS==math.MinInt64{return 0,fmt.Errorf("offset negation overflow")}
  return -h.OffsetNS,nil
 }
 return 0,fmt.Errorf("candidate does not connect requested pair")
}
type candidate struct {
 id string
 events int
 support int
 votes int
}
// Triage reduces path combinatorics by using one documented
// REPRESENTATIVE per unordered pair. All other alternatives remain
// listed, and no representative is treated as acoustically verified.
// A two-hop path through a different recording is topologically
// distinct from a parallel candidate. Every intermediary is inspected
// exactly once; there is no arbitrary 24-path truncation.
func Triage(p *projectstate.Project)(TriageReport,error){
 if err:=p.Validate();err!=nil{return TriageReport{},err}
 names:=make([]string,0,len(p.Recordings))
 for id:=range p.Recordings{names=append(names,id)}
 sort.Strings(names)
 events:=make(map[string]map[string]bool)
 for _,o:=range p.Observations{
  if events[o.HypothesisID]==nil{events[o.HypothesisID]=map[string]bool{}}
  events[o.HypothesisID][o.EventID]=true
 }
 groups:=make(map[pairKey][]candidate)
 count:=0
 for id,h:=range p.Hypotheses{
  if h.Status==projectstate.Rejected{continue}
  count++
  if h.OffsetNS==math.MinInt64{return TriageReport{},fmt.Errorf("invalid reverse offset %q",id)}
  c:=candidate{id:id,events:len(events[id])}
  if h.Discovery!=nil{
   c.support=h.Discovery.SupportWindows;c.votes=h.Discovery.Votes
  }
  k:=canonical(h.Reference,h.Query)
  groups[k]=append(groups[k],c)
 }
 keys:=make([]pairKey,0,len(groups))
 for k:=range groups{keys=append(keys,k)}
 sort.Slice(keys,func(i,j int)bool{
  if keys[i].first!=keys[j].first{return keys[i].first<keys[j].first}
  return keys[i].second<keys[j].second
 })
 representative:=make(map[pairKey]string,len(keys))
 altCount:=0
 report:=TriageReport{
  Status:"GRAPH_TOPOLOGY_TRIAGE_NOT_CERTIFIED",
  Recordings:len(names),InputCandidates:count,PairGroups:len(keys),
  Representatives:len(keys),
  RepresentativePolicy:"distinct_event_ids_then_discovery_support_then_votes_then_lexical_id; ranking_only_not_verification",
  Certification:"not_assessed",
  Pairs:make([]TriagePair,0,len(keys)),
 }
 for _,k:=range keys{
  candidates:=groups[k]
  sort.Slice(candidates,func(i,j int)bool{
   a,b:=candidates[i],candidates[j]
   if a.events!=b.events{return a.events>b.events}
   if a.support!=b.support{return a.support>b.support}
   if a.votes!=b.votes{return a.votes>b.votes}
   return a.id<b.id
  })
  lead:=candidates[0]
  representative[k]=lead.id
  item:=TriagePair{
   First:k.first,Second:k.second,RepresentativeID:lead.id,
   DistinctObservationEvents:lead.events,
   DiscoverySupportWindows:lead.support,
   AlternateHypothesisIDs:make([]string,0,len(candidates)-1),
   Paths:[]TriagePath{},
  }
  for _,c:=range candidates[1:]{
   item.AlternateHypothesisIDs=append(item.AlternateHypothesisIDs,c.id)
   altCount++
  }
  report.Pairs=append(report.Pairs,item)
 }
 report.AlternativeHypothesesRetained=altCount
 // Topology of diagnostic representatives only. Isolated recordings
 // remain explicitly present and are not marked unsyncable.
 adjacency:=make(map[string][]string)
 for _,k:=range keys{
  adjacency[k.first]=append(adjacency[k.first],k.second)
  adjacency[k.second]=append(adjacency[k.second],k.first)
 }
 seen:=map[string]bool{}
 for _,start:=range names{
  if seen[start]{continue}
  seen[start]=true
  stack:=[]string{start};component:=[]string{}
  for len(stack)>0{
   id:=stack[len(stack)-1];stack=stack[:len(stack)-1]
   component=append(component,id)
   for _,neighbor:=range adjacency[id]{
    if !seen[neighbor]{seen[neighbor]=true;stack=append(stack,neighbor)}
   }
  }
  sort.Strings(component)
  report.ConnectedComponents=append(report.ConnectedComponents,component)
 }
 // Bound the work structurally: one two-hop path for each intermediate
 // recording. Other hypotheses remain available as alternatives.
 for i:=range report.Pairs{
  item:=&report.Pairs[i]
  direct:=p.Hypotheses[item.RepresentativeID]
  wanted,err:=directedOffset(direct,item.First,item.Second)
  if err!=nil{return TriageReport{},err}
  for _,mid:=range names{
   if mid==item.First||mid==item.Second{continue}
   firstID,okA:=representative[canonical(item.First,mid)]
   secondID,okB:=representative[canonical(mid,item.Second)]
   if !okA||!okB{continue}
   first,err:=directedOffset(p.Hypotheses[firstID],item.First,mid)
   if err!=nil{return TriageReport{},err}
   second,err:=directedOffset(p.Hypotheses[secondID],mid,item.Second)
   if err!=nil{return TriageReport{},err}
   predicted,err:=addInt(first,second)
   if err!=nil{return TriageReport{},err}
   residual,err:=addInt(wanted,-predicted)
   if err!=nil{return TriageReport{},err}
   item.Paths=append(item.Paths,TriagePath{
    Via:mid,FirstHypothesisID:firstID,SecondHypothesisID:secondID,
    PredictedOffsetNS:predicted,ResidualNS:residual,
   })
  }
  report.TwoHopPathCount+=len(item.Paths)
  if len(item.Paths)==0{report.PairsWithoutPath++}
 }
 return report,nil
}
