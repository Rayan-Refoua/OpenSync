package autosync

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

type MediaRecord struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Source         string         `json:"source"`
	LocalSource    string         `json:"local_source"`
	Cache          string         `json:"cache"`
	CacheSHA       string         `json:"cache_sha256"`
	Duration       float64        `json:"duration"`
	SourceBytes    int64          `json:"source_bytes"`
	SourceMtime    int64          `json:"source_mtime_ns"`
	Video          map[string]any `json:"video"`
	Audio          map[string]any `json:"audio"`
	SourceTimecode map[string]any `json:"source_timecode"`
	FormatTags     map[string]any `json:"format_tags,omitempty"`
	SourceGroup    string         `json:"source_group,omitempty"`
	AnalysisAudio  string         `json:"analysis_audio,omitempty"`
	CacheReused    bool           `json:"cache_reused"`
}
type TimeModel struct {
	PlacementUncertainty float64     `json:"placement_uncertainty_seconds,omitempty"`
	TimingBasis          string      `json:"timing_basis,omitempty"`
	Recording            int         `json:"recording"`
	Component            int         `json:"component"`
	ComponentSize        int         `json:"component_size"`
	Offset               float64     `json:"component_offset_seconds"`
	AnalysisOffset       float64     `json:"analysis_component_offset_seconds"`
	AudioOrigin          float64     `json:"analysis_audio_origin_seconds"`
	Rate                 float64     `json:"rate"`
	Error                float64     `json:"fit_residual_seconds"`
	Status               string      `json:"status"`
	Knots                []TimePoint `json:"timing_knots"`
	Resolved             bool        `json:"globally_resolved"`
}
type NativePayload struct {
	Name    string         `json:"name"`
	Engine  string         `json:"engine_version"`
	Records []MediaRecord  `json:"recordings"`
	Models  []TimeModel    `json:"timing_models"`
	Graph   map[string]any `json:"graph"`
}
type NativeProject struct {
	Format  string          `json:"format"`
	Schema  int             `json:"schema_version"`
	Payload json.RawMessage `json:"payload"`
	Hash    string          `json:"content_sha256"`
	Data    NativePayload   `json:"-"`
}

func number(v any) float64 {
	switch x := v.(type) {
	case json.Number:
		f, _ := x.Float64()
		return f
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
}
func stringValue(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func canonicalJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	var emit func(any) error
	emit = func(v any) error {
		switch x := v.(type) {
		case string:
			b.WriteByte('"')
			for _, r := range x {
				switch r {
				case '"':
					b.WriteString("\\\"")
				case '\\':
					b.WriteString("\\\\")
				case '\b':
					b.WriteString("\\b")
				case '\f':
					b.WriteString("\\f")
				case '\n':
					b.WriteString("\\n")
				case '\r':
					b.WriteString("\\r")
				case '\t':
					b.WriteString("\\t")
				default:
					if r < 32 {
						fmt.Fprintf(&b, "\\u%04x", r)
					} else {
						b.WriteRune(r)
					}
				}
			}
			b.WriteByte('"')
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			b.WriteByte('{')
			for i, k := range keys {
				if i > 0 {
					b.WriteByte(',')
				}
				if err := emit(k); err != nil {
					return err
				}
				b.WriteByte(':')
				if err := emit(x[k]); err != nil {
					return err
				}
			}
			b.WriteByte('}')
		case []any:
			b.WriteByte('[')
			for i, a := range x {
				if i > 0 {
					b.WriteByte(',')
				}
				if err := emit(a); err != nil {
					return err
				}
			}
			b.WriteByte(']')
		case json.Number:
			if strings.ContainsAny(string(x), ".eE") {
				v, err := x.Float64()
				if err != nil {
					return err
				}
				b.WriteString(pythonFloat(v))
			} else {
				b.WriteString(string(x))
			}
		default:
			var one bytes.Buffer
			e := json.NewEncoder(&one)
			e.SetEscapeHTML(false)
			if err := e.Encode(v); err != nil {
				return err
			}
			b.Write(bytes.TrimSuffix(one.Bytes(), []byte{'\n'}))
		}
		return nil
	}
	if err := emit(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
func LoadNativeProject(path string) (*NativeProject, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeNativeProject(path, data)
}
func decodeNativeProject(path string, data []byte) (*NativeProject, error) {
	var err error
	var p NativeProject
	if err = json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	if p.Format == "opensync.solution" {
		return loadSolution(path, data)
	}
	if p.Format != "opensync.project" || (p.Schema != 1 && p.Schema != 2) {
		return nil, fmt.Errorf("unsupported project format/version")
	}
	var value any
	d := json.NewDecoder(bytes.NewReader(p.Payload))
	d.UseNumber()
	if err = d.Decode(&value); err != nil {
		return nil, err
	}
	canonical, err := canonicalJSON(value)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(canonical)
	if hex.EncodeToString(sum[:]) != p.Hash {
		return nil, fmt.Errorf("native project integrity mismatch")
	}
	d = json.NewDecoder(bytes.NewReader(p.Payload))
	d.UseNumber()
	if err = d.Decode(&p.Data); err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	ids := map[string]bool{}
	for _, r := range p.Data.Records {
		if r.ID == "" || ids[r.ID] || r.Duration <= 0 || math.IsNaN(r.Duration) || math.IsInf(r.Duration, 0) {
			return nil, fmt.Errorf("invalid source inventory")
		}
		ids[r.ID] = true
	}
	components, ok := p.Data.Graph["components"].([]any)
	if !ok {
		return nil, fmt.Errorf("missing project component identities")
	}
	for _, m := range p.Data.Models {
		if m.PlacementUncertainty < 0 || math.IsNaN(m.PlacementUncertainty) || math.IsInf(m.PlacementUncertainty, 0) {
			return nil, fmt.Errorf("invalid placement uncertainty")
		}
		if m.Status == "provisional_chronological_estimate" && (m.PlacementUncertainty < 1 || m.TimingBasis == "" || m.TimingBasis == "acoustic_arrival") {
			return nil, fmt.Errorf("chronological estimate lacks explicit uncertainty or provenance")
		}
		if m.Recording < 0 || m.Recording >= len(p.Data.Records) || seen[m.Recording] || m.Rate <= 0 || math.IsNaN(m.Rate) || math.IsInf(m.Rate, 0) || math.IsNaN(m.Offset) || math.IsInf(m.Offset, 0) {
			return nil, fmt.Errorf("invalid timing model")
		}
		seen[m.Recording] = true
		if m.Component < 1 || m.Component > len(components) {
			return nil, fmt.Errorf("invalid component")
		}
		group, ok := components[m.Component-1].([]any)
		found := false
		for _, v := range group {
			if int(number(v)) == m.Recording {
				found = true
			}
		}
		if !ok || !found || m.ComponentSize != len(group) {
			return nil, fmt.Errorf("project component identity mismatch")
		}
		if len(m.Knots) > 0 {
			if err = ValidateMapping(m.Knots); err != nil {
				return nil, err
			}
		}
	}
	if len(seen) != len(p.Data.Records) {
		return nil, fmt.Errorf("timing model inventory mismatch")
	}
	return &p, nil
}

func nativeModelMaps(records []MediaRecord, layout []NativeLayout, graph map[string]any) ([]map[string]any, int) {
	chronology := map[int]ChronologicalPlacement{}
	if placements, ok := graph["chronological_placements"].([]ChronologicalPlacement); ok {
		for _, p := range placements {
			chronology[p.Recording] = p
		}
	}
	models := []map[string]any{}
	schema := 1
	for _, item := range layout {
		r := records[item.Recording]
		origin := 0.
		if r.Video != nil && r.Audio != nil {
			origin = number(r.Audio["start_time"]) - number(r.Video["start_time"])
		}
		m := map[string]any{"recording": item.Recording, "component": item.Component, "component_size": item.ComponentSize, "analysis_component_offset_seconds": item.Start, "analysis_audio_origin_seconds": origin, "component_offset_seconds": item.Start - item.Rate*origin, "rate": item.Rate, "fit_residual_seconds": item.Error, "status": item.Status, "time_domain": fmt.Sprintf("component-%d", item.Component), "globally_resolved": !strings.HasPrefix(item.Status, "unresolved"), "timing_basis": "acoustic_arrival", "physical_capture_alignment": "not_independently_established", "propagation_delay_seconds": nil}
		if len(item.Knots) > 0 {
			schema = 2
			points := append([]TimePoint(nil), item.Knots...)
			for i := range points {
				points[i][0] += origin
			}
			m["analysis_timing_knots"] = item.Knots
			m["timing_knots"] = points
			m["component_offset_seconds"] = MapTime(points, 0)
		}
		models = append(models, m)
		if p, ok := chronology[item.Recording]; ok {
			m["timing_basis"] = p.Basis
			m["placement_uncertainty_seconds"] = p.Uncertainty
			m["acoustic_verification"] = "not_established_for_this_placement"
			m["uncertainty_interpretation"] = "conditional_estimate_not_statistical_confidence_bound"
		}
	}
	return models, schema
}
func saveNativeProject(path, name string, records []MediaRecord, edges []*PairEvidence, layout []NativeLayout, graph map[string]any) error {
	models, schema := nativeModelMaps(records, layout, graph)
	payload := map[string]any{"name": name, "engine_version": "opensync-autonomous-v1", "implementation": "native-go-v1", "recordings": records, "timing_models": models, "validated_relations": edges, "graph": graph, "timing_precision": "IEEE-754 binary64 seconds, affine rates and monotonic timing knots; no timeline frame quantization", "verification_status": "acoustic_and_graph_evidence_supported; production_import_review_pending", "acoustic_environment_policy": map[string]any{"constant_path_delay": "absorbed_in_measured_offset_not_clock_drift", "changing_path_or_echo": "requires_timing_evidence_not_blanket_tolerance", "physical_event_time": "not_identifiable_from_unreferenced_audio_alone"}, "source_media_modified": false}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	raw, err := rawObject(data)
	if err != nil {
		return err
	}
	canonical, err := canonicalJSON(raw)
	if err != nil {
		return err
	}
	project := NativeProject{Format: "opensync.project", Schema: schema, Payload: canonical, Hash: shaBytes(canonical)}
	return writeJSON(path, project)
}
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(data, '\n'))
}
func atomicWrite(path string, data []byte) error {
	tmp := path + ".partial"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
