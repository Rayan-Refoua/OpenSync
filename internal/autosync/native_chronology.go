package autosync

import (
	"encoding/json"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// These are placement estimates, deliberately separate from acoustic edges,
// fit residuals and certification. Bounds are conditional on a stable clock /
// recording series, not a calibrated statistical confidence interval.
type ChronologicalPlacement struct {
	Recording   int     `json:"recording"`
	Start       float64 `json:"start_seconds"`
	Uncertainty float64 `json:"placement_uncertainty_seconds"`
	Basis       string  `json:"timing_basis"`
	Anchors     []int   `json:"anchors"`
	Status      string  `json:"status"`
}

var sequenceName = regexp.MustCompile(`^(.*?)([0-9]+)([^0-9]*)$`)
var recordingChannel = regexp.MustCompile(`(?i)[ _.-](?:tr|track|ch|channel)[ _.-]*0*[0-9]+$`)

func recordingSeriesName(name string) (family string, sequence int64, ok bool) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	// Audio channel numbers are fixed suffixes, not recording counters.
	channel := recordingChannel.FindString(stem)
	stem = strings.TrimSuffix(stem, channel)
	match := sequenceName.FindStringSubmatch(stem)
	if match == nil {
		return "", 0, false
	}
	sequence, err := strconv.ParseInt(match[2], 10, 64)
	if err != nil {
		return "", 0, false
	}
	normalize := func(s string) string {
		return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
			return r == '_' || r == '-' || r == '.' || unicode.IsSpace(r)
		}), " ")
	}
	prefix, suffix := normalize(match[1]), normalize(match[3]+channel)
	pattern := strings.TrimSpace(prefix + " # " + suffix)
	return pattern + strings.ToLower(ext), sequence, true
}

// TrackFamily groups filename patterns for presentation. It supplies no
// acoustic timing constraints.
func TrackFamily(name string) string {
	family, _, _ := recordingSeriesName(name)
	return family
}

func recordingStamp(r MediaRecord, kind string) (float64, bool) {
	if kind == "filesystem_mtime" {
		return float64(r.SourceMtime) / 1e9, r.SourceMtime > 0
	}
	var text string
	if kind == "sidecar_creation" {
		text = stringValue(r.SourceTimecode["creation_time"])
	} else {
		stream := r.Video
		if stream == nil {
			stream = r.Audio
		}
		tags, _ := stream["tags"].(map[string]any)
		text = stringValue(tags["creation_time"])
		if text == "" {
			text = stringValue(r.FormatTags["creation_time"])
		}
	}
	t, err := time.Parse(time.RFC3339Nano, text)
	return float64(t.Unix()) + float64(t.Nanosecond())/1e9, err == nil
}

func sourceOrigin(r MediaRecord) float64 {
	if r.Video != nil && r.Audio != nil {
		return number(r.Audio["start_time"]) - number(r.Video["start_time"])
	}
	return 0
}

type continuityEntry struct {
	id       int
	sequence int64
}

func continuitySeries(records []MediaRecord) (map[string][]continuityEntry, []string) {

	groups := map[string][]continuityEntry{}
	for i, r := range records {
		family, n, ok := recordingSeriesName(r.Name)
		if !ok || (r.SourceGroup == "" && r.Source == "") {
			continue
		}
		// Retain physical directory boundaries even in a broad imported group.
		key := strings.ToLower(filepath.Dir(r.Source)+"\x00"+filepath.Clean(r.SourceGroup)) + "\x00" + family
		groups[key] = append(groups[key], continuityEntry{i, n})
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, series := range groups {
		sort.Slice(series, func(i, j int) bool {
			if series[i].sequence != series[j].sequence {
				return series[i].sequence < series[j].sequence
			}
			return series[i].id < series[j].id
		})
	}
	return groups, keys
}

type ContinuityLink struct {
	Before          string `json:"before"`
	After           string `json:"after"`
	Relation        string `json:"relation"`
	Basis           string `json:"basis"`
	Status          string `json:"status"`
	MissingCounters int64  `json:"missing_counters,omitempty"`
}

func continuityEvidence(records []MediaRecord, layout []NativeLayout) []ContinuityLink {
	groups, keys := continuitySeries(records)
	models := make(map[int]NativeLayout, len(layout))
	for _, m := range layout {
		models[m.Recording] = m
	}
	links := []ContinuityLink{}
	for _, key := range keys {
		series := groups[key]
		for i := 1; i < len(series); i++ {
			a, b := series[i-1], series[i]
			link := ContinuityLink{Before: records[a.id].ID, After: records[b.id].ID, Relation: "successor", Basis: "source_group_and_filename_counter", Status: "proposed"}
			if a.sequence == b.sequence || (i > 1 && series[i-2].sequence == a.sequence) || (i+1 < len(series) && series[i+1].sequence == b.sequence) {
				link.Relation = "same_recorder_candidate"
				link.Status = "unresolved"
			} else {
				link.MissingCounters = b.sequence - a.sequence - 1
				x, xok := models[a.id]
				y, yok := models[b.id]
				if xok && yok && x.Component == y.Component && x.ComponentSize > 1 && x.Start > y.Start+x.Error+y.Error {
					link.Status = "contradicted"
					link.Basis += "_conflicts_with_acoustic_order"
				}
			}
			links = append(links, link)
		}
	}
	return links
}

func chronologicalPlacements(records []MediaRecord, layout []NativeLayout, blocked map[int]bool) []ChronologicalPlacement {
	byID := map[int]NativeLayout{}
	for _, m := range layout {
		byID[m.Recording] = m
	}
	groups, keys := continuitySeries(records)
	var result []ChronologicalPlacement
	for _, key := range keys {
		series := groups[key]
		duplicate := false
		for i := 1; i < len(series); i++ {
			duplicate = duplicate || series[i].sequence == series[i-1].sequence
		}
		if duplicate {
			continue
		}
		isAnchor := func(id int) bool {
			m := byID[id]
			return m.Component == 1 && m.ComponentSize > 1 && m.Status == "acoustically_supported_not_certified"
		}
		start := func(id int) float64 { return MapTime(byID[id].Knots, 0) - byID[id].Rate*sourceOrigin(records[id]) }
		end := func(id int) float64 {
			d := records[id].Duration
			if v := number(records[id].Video["duration"]); v > 0 {
				d = v
			}
			return MapTime(byID[id].Knots, d-sourceOrigin(records[id]))
		}
		var anchors []int
		validOrder := true
		for _, e := range series {
			if isAnchor(e.id) {
				if len(anchors) > 0 && end(anchors[len(anchors)-1]) > start(e.id)+.1 {
					validOrder = false
				}
				anchors = append(anchors, e.id)
			}
		}
		if !validOrder {
			continue
		}
		for index, e := range series {
			m := byID[e.id]
			// Never disassemble a supported disconnected group or resolve a
			// rejected/ambiguous acoustic placement through metadata.
			if !strings.HasPrefix(m.Status, "unresolved") || m.ComponentSize != 1 || blocked[e.id] || index == 0 || index == len(series)-1 {
				continue
			}
			before, after := series[index-1], series[index+1]
			if !isAnchor(before.id) || !isAnchor(after.id) {
				continue
			}
			duration := records[e.id].Duration
			if v := number(records[e.id].Video["duration"]); v > 0 {
				duration = v
			}
			lo, hi := end(before.id), start(after.id)-duration
			if hi < lo {
				continue
			}
			var proposals []ChronologicalPlacement
			usableVaryingDates := false
			for _, kind := range []string{"sidecar_creation", "container_creation", "filesystem_mtime"} {
				stamp, ok := recordingStamp(records[e.id], kind)
				if !ok {
					continue
				}
				var times []float64
				available := true
				for _, a := range anchors {
					v, ok := recordingStamp(records[a], kind)
					if !ok {
						available = false
						break
					}
					times = append(times, v)
				}
				clockAnchors := anchors
				if !available {
					clockAnchors = []int{before.id, after.id}
					a, oka := recordingStamp(records[before.id], kind)
					b, okb := recordingStamp(records[after.id], kind)
					if !oka || !okb {
						continue
					}
					times = []float64{a, b}
				}
				if len(times) < 2 {
					continue
				}
				allSame := true
				for _, v := range times {
					if math.Abs(v-stamp) > .001 {
						allSame = false
					}
				}
				if allSame {
					continue
				}
				usableVaryingDates = true
				if len(clockAnchors) == 2 && (before.sequence+1 != e.sequence || e.sequence+1 != after.sequence) {
					continue
				}
				// Modification times can denote completion rather than start.
				// Compete both conventions; keep all plausible outcomes.
				for _, atEnd := range []bool{false, true} {
					if atEnd && kind != "filesystem_mtime" {
						continue
					}
					var offsets []float64
					maxError := 0.
					for j, a := range clockAnchors {
						actual := start(a)
						if atEnd {
							actual = end(a)
						}
						offsets = append(offsets, actual-times[j])
						maxError = math.Max(maxError, byID[a].Error)
					}
					low, high := math.Inf(1), math.Inf(-1)
					for _, v := range offsets {
						low = math.Min(low, v)
						high = math.Max(high, v)
					}
					// Leave-one-out constant clock error cannot exceed one
					// timestamp tick plus the measured anchor residual.
					used := clockAnchors
					local := false
					if high-low > 1+maxError {
						if before.sequence+1 != e.sequence || e.sequence+1 != after.sequence {
							continue
						}
						// A reset outside the bracket need not invalidate the local clock.
						used = []int{before.id, after.id}
						low, high = math.Inf(1), math.Inf(-1)
						maxError = 0
						for _, a := range used {
							stampA, _ := recordingStamp(records[a], kind)
							t := start(a)
							if atEnd {
								t = end(a)
							}
							low = math.Min(low, t-stampA)
							high = math.Max(high, t-stampA)
							maxError = math.Max(maxError, byID[a].Error)
						}
						if high-low > 1+maxError {
							continue
						}
						local = true
					}
					predicted := stamp + (low+high)/2
					if atEnd {
						predicted -= duration
					}
					uncertainty := 1 + (high-low)/2 + maxError
					if predicted < lo-uncertainty || predicted > hi+uncertainty {
						continue
					}
					left, right := math.Max(lo, predicted-uncertainty), math.Min(hi, predicted+uncertainty)
					basis := "calibrated_" + kind
					if local {
						basis += "_local"
					}
					if atEnd {
						basis += "_end"
					}
					proposals = append(proposals, ChronologicalPlacement{e.id, (left + right) / 2, math.Max(1, (right-left)/2+maxError), basis, append([]int(nil), used...), "provisional_chronological_estimate"})
				}
			}
			if len(proposals) > 0 {
				// Intersect corroborating clocks. Conflicting clocks are not
				// resolved by picking whichever produces more coverage.
				left, right := math.Inf(-1), math.Inf(1)
				for _, p := range proposals {
					left = math.Max(left, p.Start-p.Uncertainty)
					right = math.Min(right, p.Start+p.Uncertainty)
				}
				if left <= right {
					p := proposals[0]
					p.Start = (math.Max(lo, left) + math.Min(hi, right)) / 2
					p.Uncertainty = math.Max(1, (right-left)/2)
					result = append(result, p)
				}
			} else if !usableVaryingDates && before.sequence+1 == e.sequence && e.sequence+1 == after.sequence && hi-lo <= 2 {
				// One missing consecutive recording, bounded by two acoustic
				// anchors. Do not concatenate arbitrary runs or fill large gaps.
				result = append(result, ChronologicalPlacement{e.id, (lo + hi) / 2, math.Max(1, (hi-lo)/2+math.Max(byID[before.id].Error, byID[after.id].Error)), "bounded_sequence_gap", []int{before.id, after.id}, "provisional_chronological_estimate"})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Recording < result[j].Recording })
	return result
}

// Rehome estimated singleton placements without changing any acoustic edge or
// another recording's timing model. A metadata estimate never becomes an anchor.
func applyChronology(records []MediaRecord, layout []NativeLayout, graph map[string]any) []NativeLayout {
	blocked := map[int]bool{}
	var decisions []map[string]any
	if b, err := json.Marshal(graph["event_decisions"]); err == nil {
		_ = json.Unmarshal(b, &decisions)
	}
	if decisions != nil {
		for _, d := range decisions {
			if d["status"] == "unresolved_competing_events" {
				blocked[int(number(d["recording"]))] = true
			}
		}
	}
	var ledger ReviewLedger
	if b, err := json.Marshal(graph["review_ledger"]); err == nil && json.Unmarshal(b, &ledger) == nil {
		for i, r := range records {
			for _, id := range ledger.Recordings {
				if r.ID == id {
					blocked[i] = true
				}
			}
			for _, p := range ledger.Pairs {
				if r.ID == p.A || r.ID == p.B {
					blocked[i] = true
				}
			}
		}
	}
	placements := chronologicalPlacements(records, layout, blocked)
	graph["chronological_placements"] = placements
	if len(placements) == 0 {
		return layout
	}
	result := append([]NativeLayout(nil), layout...)
	byID := map[int]ChronologicalPlacement{}
	for _, p := range placements {
		byID[p.Recording] = p
	}
	groups := [][]int{}
	oldGroups := graph["components"].([][]int)
	for ci, g := range oldGroups {
		var group []int
		for _, id := range g {
			if _, moved := byID[id]; !moved {
				group = append(group, id)
			}
		}
		if ci == 0 {
			for _, p := range placements {
				group = append(group, p.Recording)
			}
			sort.Ints(group)
		}
		if len(group) > 0 {
			groups = append(groups, group)
		}
	}
	for i, m := range result {
		if p, ok := byID[m.Recording]; ok {
			m.Start = p.Start + sourceOrigin(records[m.Recording])
			m.Rate = 1
			m.Error = 0
			m.Knots = []TimePoint{{0, m.Start}, {records[m.Recording].Duration, m.Start + records[m.Recording].Duration}}
			m.Status = p.Status
		}
		for ci, g := range groups {
			for _, id := range g {
				if id == m.Recording {
					m.Component = ci + 1
					m.ComponentSize = len(g)
				}
			}
		}
		result[i] = m
	}
	graph["acoustic_components"] = oldGroups
	graph["components"] = groups
	return result
}
