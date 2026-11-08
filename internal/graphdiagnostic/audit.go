// Package graphdiagnostic audits alternative paths without certifying any match.
// Relative offsets use start(query) - start(reference) = offset_ns.
package graphdiagnostic

import (
	"fmt"
	"math"
	"sort"

	"opensync/internal/projectstate"
)

type Path struct {
	Nodes []string `json:"nodes"`
	HypothesisIDs []string `json:"hypothesis_ids"`
	PredictedOffsetNS int64 `json:"predicted_offset_ns"`
	ResidualNS int64 `json:"residual_ns"`
	Statuses []projectstate.Status `json:"statuses"`
}
type EdgeAudit struct {
	HypothesisID string `json:"hypothesis_id"`
	Reference string `json:"reference"`
	Query string `json:"query"`
	OffsetNS int64 `json:"offset_ns"`
	Status string `json:"status"`
	AlternativePaths []Path `json:"alternative_paths"`
	PathsTruncated bool `json:"paths_truncated"`
}
type Report struct {
	Status string `json:"status"`
	RecordingCount int `json:"recording_count"`
	CandidateCount int `json:"candidate_count"`
	ConnectedComponents [][]string `json:"connected_components"`
	Edges []EdgeAudit `json:"edges"`
	AcousticCertification string `json:"acoustic_certification"`
}
type arc struct {to,id string; step int64; status projectstate.Status}
const maxPathHops=3
const maxPathsPerEdge=24

// Audit enumerates short alternate acoustic-hypothesis paths. A direct
// competing hypothesis for the same recording pair does NOT qualify as a
// redundant route. Alternative routes are themselves unverified unless a
// separate acoustic certification engine establishes their provenance.
// Residuals are raw measurements: no arbitrary tolerance threshold is used.
func Audit(p *projectstate.Project)(Report,error){
	if err:=p.Validate();err!=nil{return Report{},err}
	nodes:=make([]string,0,len(p.Recordings))
	for id:=range p.Recordings{nodes=append(nodes,id)}
	sort.Strings(nodes)
	ids:=make([]string,0,len(p.Hypotheses))
	for id,h:=range p.Hypotheses{
		if h.Status!=projectstate.Rejected{ids=append(ids,id)}
	}
	sort.Strings(ids)
	adj:=make(map[string][]arc,len(nodes))
	for _,id:=range ids{
		h:=p.Hypotheses[id]
		if h.OffsetNS==math.MinInt64{return Report{},fmt.Errorf("offset cannot be negated for %q",id)}
		adj[h.Reference]=append(adj[h.Reference],arc{to:h.Query,id:id,step:h.OffsetNS,status:h.Status})
		adj[h.Query]=append(adj[h.Query],arc{to:h.Reference,id:id,step:-h.OffsetNS,status:h.Status})
	}
	for key:=range adj{
		sort.Slice(adj[key],func(i,j int)bool{
			a,b:=adj[key][i],adj[key][j]
			if a.to!=b.to{return a.to<b.to}
			return a.id<b.id
		})
	}
	// Connected components include recordings with no discovery edges.
	visited:=map[string]bool{}
	components:=make([][]string,0)
	for _,id:=range nodes{
		if visited[id]{continue}
		stack:=[]string{id}
		visited[id]=true
		component:=[]string{}
		for len(stack)>0{
			v:=stack[len(stack)-1];stack=stack[:len(stack)-1]
			component=append(component,v)
			for _,a:=range adj[v] {
				if !visited[a.to]{visited[a.to]=true;stack=append(stack,a.to)}
			}
		}
		sort.Strings(component);components=append(components,component)
	}
	report:=Report{Status:"GRAPH_ALTERNATIVE_PATH_DIAGNOSTIC_NOT_CERTIFIED",
		RecordingCount:len(nodes),CandidateCount:len(ids),
		ConnectedComponents:components,
		Edges:make([]EdgeAudit,0,len(ids)),
		AcousticCertification:"not_assessed",
	}
	for _,id:=range ids{
		h:=p.Hypotheses[id]
		entry:=EdgeAudit{HypothesisID:id,Reference:h.Reference,Query:h.Query,
			OffsetNS:h.OffsetNS,Status:"no_independent_alternative_path",
			AlternativePaths:[]Path{}}
		seen:=map[string]bool{h.Reference:true}
		var walk func(string,int64,[]string,[]string,[]projectstate.Status)
		walk=func(node string,total int64,pathNodes,pathIDs []string,statuses []projectstate.Status){
			if entry.PathsTruncated{return}
			if node==h.Query{
				if len(pathIDs)<2{return}
				if len(entry.AlternativePaths)>=maxPathsPerEdge{
					entry.PathsTruncated=true
					return
				}
				if (total>0&&h.OffsetNS<math.MinInt64+total)||
					(total<0&&h.OffsetNS>math.MaxInt64+total){entry.PathsTruncated=true;return}
				entry.AlternativePaths=append(entry.AlternativePaths,Path{
					Nodes:append([]string(nil),pathNodes...),
					HypothesisIDs:append([]string(nil),pathIDs...),
					PredictedOffsetNS:total,ResidualNS:h.OffsetNS-total,
					Statuses:append([]projectstate.Status(nil),statuses...),
				})
				return
			}
			if len(pathIDs)>=maxPathHops{return}
			for _,a:=range adj[node]{
				// Exclude all alternate hypotheses of the exact same pair
				// from constituting redundant graph evidence.
				if (node==h.Reference&&a.to==h.Query)||
					(node==h.Query&&a.to==h.Reference)||
					seen[a.to]||a.id==id{continue}
				if (a.step>0&&total>math.MaxInt64-a.step)||
					(a.step<0&&total<math.MinInt64-a.step){entry.PathsTruncated=true;return}
				seen[a.to]=true
				walk(a.to,total+a.step,
					append(pathNodes,a.to),append(pathIDs,a.id),append(statuses,a.status))
				delete(seen,a.to)
				if entry.PathsTruncated{return}
			}
		}
		walk(h.Reference,0,[]string{h.Reference},nil,nil)
		if len(entry.AlternativePaths)>0{entry.Status="alternative_paths_diagnostic_only"}
		report.Edges=append(report.Edges,entry)
	}
	return report,nil
}
