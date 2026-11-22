package autosync

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type ReviewPair struct {
	A string `json:"a"`
	B string `json:"b"`
}
type ReviewLedger struct {
	Version    int          `json:"version"`
	Recordings []string     `json:"rejected_recordings"`
	Pairs      []ReviewPair `json:"rejected_pairs"`
}
type ReviewChange struct {
	Candidate         string `json:"candidate,omitempty"`
	FileTimePlacement *bool  `json:"fileTimePlacement,omitempty"`
	Kind              string `json:"kind"`
	Recording         string `json:"recording"`
	Reference         string `json:"reference"`
	Query             string `json:"query"`
	Mode              string `json:"mode"`
}
type SolutionDelta struct {
	Base     string           `json:"base_project"`
	BaseHash string           `json:"base_content_sha256"`
	Parent   string           `json:"parent_project"`
	Created  string           `json:"created_utc"`
	Label    string           `json:"label"`
	Models   []map[string]any `json:"timing_models"`
	Graph    map[string]any   `json:"graph_changes"`
}

func projectLedger(p *NativeProject) (ReviewLedger, error) {
	l := ReviewLedger{Version: 1}
	if v, ok := p.Data.Graph["review_ledger"]; ok {
		b, err := json.Marshal(v)
		if err != nil {
			return l, err
		}
		if err = json.Unmarshal(b, &l); err != nil {
			return l, err
		}
		if l.Version != 1 {
			return l, fmt.Errorf("unsupported correction ledger version")
		}
	}
	return l, nil
}

func correctionsForRecords(path string, records []MediaRecord) (ReviewLedger, error) {
	if path == "" {
		return ReviewLedger{Version: 1}, nil
	}
	p, err := LoadNativeProject(path)
	if err != nil {
		return ReviewLedger{}, err
	}
	ledger, err := projectLedger(p)
	if err != nil {
		return ledger, err
	}
	if _, _, err = reviewBlocked(records, ledger); err != nil {
		return ledger, err
	}
	required := map[string]bool{}
	for _, id := range ledger.Recordings {
		required[id] = true
	}
	for _, pair := range ledger.Pairs {
		required[pair.A] = true
		required[pair.B] = true
	}
	old := map[string]MediaRecord{}
	for _, r := range p.Data.Records {
		old[r.ID] = r
	}
	for _, r := range records {
		if required[r.ID] {
			prior, ok := old[r.ID]
			if !ok || prior.SourceBytes != r.SourceBytes || prior.SourceMtime != r.SourceMtime {
				return ledger, fmt.Errorf("source changed for manual correction: %s", r.Name)
			}
		}
	}
	return ledger, nil
}
func orderedReviewPair(a, b string) ReviewPair {
	if b < a {
		a, b = b, a
	}
	return ReviewPair{a, b}
}
func reviewBlocked(records []MediaRecord, ledger ReviewLedger) (map[int]bool, map[ReviewPair]bool, error) {
	ids := map[string]int{}
	for i, r := range records {
		ids[r.ID] = i
	}
	blocked := map[int]bool{}
	pairs := map[ReviewPair]bool{}
	for _, id := range ledger.Recordings {
		i, ok := ids[id]
		if !ok {
			return nil, nil, fmt.Errorf("correction refers to absent recording %s", id)
		}
		blocked[i] = true
	}
	for _, p := range ledger.Pairs {
		_, a := ids[p.A]
		_, b := ids[p.B]
		if !a || !b || p.A == p.B {
			return nil, nil, fmt.Errorf("invalid rejected pair")
		}
		pairs[orderedReviewPair(p.A, p.B)] = true
	}
	return blocked, pairs, nil
}
func filterReviewedEdges(records []MediaRecord, edges []*PairEvidence, ledger ReviewLedger) ([]*PairEvidence, error) {
	blocked, pairs, err := reviewBlocked(records, ledger)
	if err != nil {
		return nil, err
	}
	var keep []*PairEvidence
	for i, e := range edges {
		if e == nil || e.Reference < 0 || e.Reference >= len(records) || e.Query < 0 || e.Query >= len(records) || e.Reference == e.Query {
			return nil, fmt.Errorf("invalid saved relationship %d", i)
		}
		if !blocked[e.Reference] && !blocked[e.Query] && !pairs[orderedReviewPair(records[e.Reference].ID, records[e.Query].ID)] {
			keep = append(keep, e)
		}
	}
	return keep, nil
}
func loadSolution(path string, data []byte) (*NativeProject, error) {
	var wrapper NativeProject
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, err
	}
	if wrapper.Schema != 1 {
		return nil, fmt.Errorf("unsupported solution revision")
	}
	raw, err := rawObject(wrapper.Payload)
	if err != nil {
		return nil, err
	}
	canonical, err := canonicalJSON(raw)
	if err != nil {
		return nil, err
	}
	if shaBytes(canonical) != wrapper.Hash {
		return nil, fmt.Errorf("solution revision integrity mismatch")
	}
	var delta SolutionDelta
	if err = json.Unmarshal(wrapper.Payload, &delta); err != nil {
		return nil, err
	}
	basePath := delta.Base
	if !filepath.IsAbs(basePath) {
		basePath = filepath.Join(filepath.Dir(path), basePath)
	}
	baseData, err := os.ReadFile(basePath)
	if err != nil {
		return nil, fmt.Errorf("solution base project: %w", err)
	}
	var header struct{ Format string }
	if err = json.Unmarshal(baseData, &header); err != nil || header.Format != "opensync.project" {
		return nil, fmt.Errorf("solution must reference an original project, not another revision")
	}
	base, err := decodeNativeProject(basePath, baseData)
	if err != nil {
		return nil, err
	}
	if base.Hash != delta.BaseHash {
		return nil, fmt.Errorf("solution base project was changed")
	}
	payload, err := rawObject(base.Payload)
	if err != nil {
		return nil, err
	}
	payload["timing_models"] = delta.Models
	graph := payload["graph"].(map[string]any)
	for k, v := range delta.Graph {
		graph[k] = v
	}
	parent := delta.Parent
	if !filepath.IsAbs(parent) {
		parent = filepath.Join(filepath.Dir(path), parent)
	}
	graph["solution_revision"] = map[string]any{"base_project": basePath, "parent_project": parent, "created_utc": delta.Created, "label": delta.Label, "revision_sha256": wrapper.Hash}
	// Round-trip typed values through JSON before canonical hashing.
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	payload, err = rawObject(b)
	if err != nil {
		return nil, err
	}
	canonical, err = canonicalJSON(payload)
	if err != nil {
		return nil, err
	}
	resolved := NativeProject{Format: "opensync.project", Schema: 2, Payload: canonical, Hash: shaBytes(canonical)}
	b, err = json.Marshal(resolved)
	if err != nil {
		return nil, err
	}
	return decodeNativeProject(path, b)
}

// CreateSolution re-solves immutable saved evidence, then stores only model /
// graph changes. The original project and its acoustic measurements stay intact.
// Rejected relationships stay rejected until explicitly restored or undone.
func CreateSolution(path string, change ReviewChange) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current, err := LoadNativeProject(path)
	if err != nil {
		return "", err
	}
	basePath := path
	if v, ok := current.Data.Graph["solution_revision"].(map[string]any); ok {
		basePath = stringValue(v["base_project"])
	}
	base, err := LoadNativeProject(basePath)
	if err != nil {
		return "", err
	}
	if change.Kind == "accept_candidate" || change.Kind == "reject_candidate" {
		models, graph, label, err := reviewCandidate(current, change.Candidate, change.Kind == "accept_candidate")
		if err != nil {
			return "", err
		}
		return writeSolutionRevision(path, basePath, base.Hash, label, models, graph)
	}
	if change.Kind == "add_chronological" {
		if change.Mode != "chronological" {
			return "", fmt.Errorf("select Chronological event to add chronological placements")
		}
		models, graph, added, err := addSavedChronology(current)
		if err != nil {
			return "", err
		}
		if added == 0 {
			return path, nil
		}
		return writeSolutionRevision(path, basePath, base.Hash, fmt.Sprintf("Update %d chronological placements", added), models, graph)
	}
	ledger, err := projectLedger(current)
	if err != nil {
		return "", err
	}
	blocked, pairs, err := reviewBlocked(base.Data.Records, ledger)
	if err != nil {
		return "", err
	}
	ids := map[string]int{}
	for i, r := range base.Data.Records {
		ids[r.ID] = i
	}
	mode := stringValue(current.Data.Graph["placement_mode"])
	if mode == "" {
		mode = "clips"
	}
	label := change.Kind
	switch change.Kind {
	case "reject_recording", "restore_recording":
		i, ok := ids[change.Recording]
		if !ok {
			return "", fmt.Errorf("recording not found")
		}
		if change.Kind == "reject_recording" {
			blocked[i] = true
		} else {
			delete(blocked, i)
		}
		label += " · " + base.Data.Records[i].Name
	case "reject_pair", "restore_pair":
		_, a := ids[change.Reference]
		_, b := ids[change.Query]
		if !a || !b || change.Reference == change.Query {
			return "", fmt.Errorf("invalid recording pair")
		}
		key := orderedReviewPair(change.Reference, change.Query)
		if change.Kind == "reject_pair" {
			pairs[key] = true
		} else {
			delete(pairs, key)
		}
	case "mode":
		mode = change.Mode
	case "rebuild":
	default:
		return "", fmt.Errorf("unknown review operation")
	}
	if mode != "clips" && mode != "chronological" {
		return "", fmt.Errorf("invalid solution mode")
	}
	ledger.Recordings = nil
	ledger.Pairs = nil
	for i := range blocked {
		ledger.Recordings = append(ledger.Recordings, base.Data.Records[i].ID)
	}
	sort.Strings(ledger.Recordings)
	for p := range pairs {
		ledger.Pairs = append(ledger.Pairs, p)
	}
	sort.Slice(ledger.Pairs, func(i, j int) bool {
		if ledger.Pairs[i].A != ledger.Pairs[j].A {
			return ledger.Pairs[i].A < ledger.Pairs[j].A
		}
		return ledger.Pairs[i].B < ledger.Pairs[j].B
	})
	var evidence struct {
		Edges []*PairEvidence `json:"validated_relations"`
	}
	if err = json.Unmarshal(base.Payload, &evidence); err != nil {
		return "", err
	}
	edges, err := filterReviewedEdges(base.Data.Records, evidence.Edges, ledger)
	if err != nil {
		return "", err
	}
	layout, graph, err := nativeSolve(base.Data.Records, edges)
	if err != nil {
		return "", err
	}
	// Remap solver indices to the unchanged base evidence inventory.
	indices := map[*PairEvidence]int{}
	for i, e := range evidence.Edges {
		indices[e] = i
	}
	for i, k := range graph["active_edges"].([]int) {
		graph["active_edges"].([]int)[i] = indices[edges[k]]
	}
	for _, key := range []string{"rejected_edges", "global_evidence_residuals"} {
		for _, r := range graph[key].([]map[string]any) {
			r["edge"] = indices[edges[r["edge"].(int)]]
		}
	}
	graph["placement_mode"] = mode
	graph["recording_continuity"] = continuityEvidence(base.Data.Records, layout)
	graph["review_ledger"] = ledger
	graph["manual_candidate_decisions"] = current.Data.Graph["manual_candidate_decisions"]
	graph["event_decisions"] = base.Data.Graph["event_decisions"]
	graph["chronological_placements"] = []ChronologicalPlacement{}
	// Conservative: old provisional proposals are not automatically reinstated
	// after a manual correction changes their supporting graph.
	graph["provisional_placements"] = []any{}
	enabled, known := current.Data.Graph["file_time_placement"].(bool)
	if !known {
		enabled = true
	}
	if change.Kind == "mode" && change.FileTimePlacement != nil {
		enabled = *change.FileTimePlacement
	}
	graph["file_time_placement"] = enabled
	if mode == "chronological" && enabled {
		layout = applyChronology(base.Data.Records, layout, graph)
	}
	models, _ := nativeModelMaps(base.Data.Records, layout, graph)
	graph["unresolved_timing_hypotheses"] = base.Data.Graph["unresolved_timing_hypotheses"]
	models, graph, err = reapplyManualCandidates(base.Data.Records, models, graph)
	if err != nil {
		return "", err
	}
	return writeSolutionRevision(path, basePath, base.Hash, label, models, graph)
}

func writeSolutionRevision(path, basePath, baseHash, label string, models []map[string]any, graph map[string]any) (string, error) {
	var err error
	folder := filepath.Join(filepath.Dir(basePath), "Solutions")
	if err = os.MkdirAll(folder, 0755); err != nil {
		return "", err
	}
	baseRelative, _ := filepath.Rel(folder, basePath)
	parentRelative, _ := filepath.Rel(folder, path)
	delta := SolutionDelta{Base: baseRelative, BaseHash: baseHash, Parent: parentRelative, Created: time.Now().UTC().Format(time.RFC3339Nano), Label: label, Models: models, Graph: graph}
	b, err := json.Marshal(delta)
	if err != nil {
		return "", err
	}
	raw, err := rawObject(b)
	if err != nil {
		return "", err
	}
	canonical, err := canonicalJSON(raw)
	if err != nil {
		return "", err
	}
	wrapper := NativeProject{Format: "opensync.solution", Schema: 1, Payload: canonical, Hash: shaBytes(canonical)}
	destination := filepath.Join(folder, "solution-"+wrapper.Hash[:16]+".opensync")
	b, err = json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	_, err = f.Write(b)
	closeErr := f.Close()
	if err != nil {
		_ = os.Remove(destination)
		return "", err
	}
	if closeErr != nil {
		_ = os.Remove(destination)
		return "", closeErr
	}
	return destination, nil
}

func addSavedChronology(project *NativeProject) ([]map[string]any, map[string]any, int, error) {
	var payload struct {
		Models []map[string]any `json:"timing_models"`
		Graph  map[string]any   `json:"graph"`
	}
	if err := json.Unmarshal(project.Payload, &payload); err != nil {
		return nil, nil, 0, err
	}
	g := payload.Graph
	var groups [][]int
	raw, err := json.Marshal(g["components"])
	if err != nil {
		return nil, nil, 0, err
	}
	if err = json.Unmarshal(raw, &groups); err != nil || len(groups) == 0 {
		return nil, nil, 0, fmt.Errorf("missing project components")
	}
	g["components"] = groups
	var old []ChronologicalPlacement
	raw, err = json.Marshal(g["chronological_placements"])
	if err != nil {
		return nil, nil, 0, err
	}
	if err = json.Unmarshal(raw, &old); err != nil {
		return nil, nil, 0, err
	}
	var layout []NativeLayout
	for _, m := range ReviewTimingModels(project.Data) {
		points := append([]TimePoint(nil), m.Knots...)
		for i := range points {
			points[i][0] -= m.AudioOrigin
		}
		if len(points) == 0 {
			points = []TimePoint{{0, m.AnalysisOffset}, {1, m.AnalysisOffset + m.Rate}}
		}
		layout = append(layout, NativeLayout{Recording: m.Recording, Component: m.Component, ComponentSize: m.ComponentSize, Start: m.AnalysisOffset, Rate: m.Rate, Error: m.Error, Knots: points, Status: m.Status})
	}
	previousGroups := g["acoustic_components"]
	updated := applyChronology(project.Data.Records, layout, g)
	additions := g["chronological_placements"].([]ChronologicalPlacement)
	workspace := &nativeWorkspace{data: map[string][]byte{}, limit: 512 << 20, audio: newDirectAudio()}
	defer workspace.audio.Close()
	analyzer := NewNativeAnalyzer(project.Data.Records)
	defer analyzer.Close()
	analyzer.ctx = context.WithValue(context.Background(), workspaceKey{}, workspace)
	placements := append(old, additions...)
	refineChronologicalAudio(analyzer, updated, placements)
	moved := map[int]bool{}
	for _, p := range additions {
		moved[p.Recording] = true
	}
	for i := range updated {
		item := &updated[i]
		for _, p := range placements {
			if p.Recording != item.Recording || item.Status != "provisional_chronological_estimate" || p.Basis != "chronological_estimate_refined_by_audio" {
				continue
			}
			start := p.Start + sourceOrigin(project.Data.Records[item.Recording])
			if start != item.Start {
				delta := start - item.Start
				item.Start = start
				for j := range item.Knots {
					item.Knots[j][1] += delta
				}
				moved[item.Recording] = true
			}
		}
	}
	if len(moved) == 0 {
		return nil, nil, 0, nil
	}
	if previousGroups != nil {
		g["acoustic_components"] = previousGroups
	}
	g["chronological_placements"] = placements
	byID := map[int]map[string]any{}
	for _, model := range payload.Models {
		byID[int(number(model["recording"]))] = model
	}
	for _, item := range updated {
		m := byID[item.Recording]
		if moved[item.Recording] {
			generated, _ := nativeModelMaps(project.Data.Records, []NativeLayout{item}, g)
			for k, v := range generated[0] {
				m[k] = v
			}
		} else {
			m["component"] = item.Component
			m["component_size"] = item.ComponentSize
			m["time_domain"] = fmt.Sprintf("component-%d", item.Component)
		}
	}

	g["file_time_placement"] = true
	g["placement_mode"] = "chronological"
	delete(g, "solution_revision")
	return payload.Models, g, len(moved), nil
}

func NativeReviseCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("revise requires a saved .opensync project")
	}
	f := flag.NewFlagSet("revise", flag.ContinueOnError)
	change := ReviewChange{Kind: "rebuild"}
	add := f.Bool("add-chronological", false, "add chronological placements to the current revision with --mode chronological")
	f.StringVar(&change.Mode, "mode", "", "clips or chronological")
	f.StringVar(&change.Recording, "reject-recording", "", "recording ID to keep unresolved")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected revise argument")
	}
	if change.Mode != "" && change.Recording != "" {
		return fmt.Errorf("apply mode and recording corrections as separate revisions")
	}
	if change.Mode != "" {
		change.Kind = "mode"
	}
	if change.Recording != "" {
		change.Kind = "reject_recording"
	}
	if *add {
		if change.Recording != "" {
			return fmt.Errorf("add placements separately from rejecting a recording")
		}
		change.Kind = "add_chronological"
	}
	path, err := CreateSolution(args[0], change)
	if err == nil {
		fmt.Println("PROJECT", path)
	}
	return err
}
