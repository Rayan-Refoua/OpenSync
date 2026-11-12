package autosync

import (
	"encoding/xml"
	"flag"
	"fmt"
	"math"
	"net/url"
	"os"
	"sort"
	"strings"
)

type importItem struct {
	Name    string  `json:"name"`
	Kind    string  `json:"kind"`
	Source  string  `json:"source"`
	Rate    float64 `json:"rate"`
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	In      float64 `json:"source_in"`
	Out     float64 `json:"source_out"`
	Channel int     `json:"channel"`
}

func importSnapshot(path string) (map[string][]importItem, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root xnode
	if err = xml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	files := map[string]*xnode{}
	for _, f := range root.all("file") {
		if f.get("pathurl") != nil {
			files[f.attribute("id")] = f
		}
	}
	sequences := map[string][]importItem{}
	for _, seq := range root.all("sequence") {
		rate, err := xmlRate(seq)
		if err != nil {
			return nil, err
		}
		items := []importItem{}
		for _, kind := range []string{"video", "audio"} {
			section := seq.get("media/" + kind)
			if section == nil {
				continue
			}
			for _, track := range section.Children {
				if track.Name != "track" {
					continue
				}
				for _, clip := range track.Children {
					if clip.Name != "clipitem" {
						continue
					}
					f := clip.get("file")
					if f == nil {
						continue
					}
					definition := files[f.attribute("id")]
					if definition == nil {
						definition = f
					}
					u, err := url.Parse(definition.text("pathurl"))
					if err != nil {
						return nil, err
					}
					identity := u.Path
					if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
						identity = "//" + u.Host + identity
					}
					identity = strings.ToLower(strings.ReplaceAll(identity, "\\", "/"))
					sourceRate, err := xmlRate(clip)
					if clip.get("rate") == nil {
						sourceRate, err = xmlRate(definition)
					}
					if err != nil {
						return nil, err
					}
					values := make([]int64, 4)
					for i, key := range []string{"start", "end", "in", "out"} {
						values[i], err = xmlInt(clip, key)
						if err != nil {
							return nil, err
						}
					}
					channel, _ := xmlInt(clip, "sourcetrack/trackindex")
					items = append(items, importItem{clip.text("name"), kind, identity, sourceRate.FPS(), float64(values[0]) / rate.FPS(), float64(values[1]) / rate.FPS(), float64(values[2]) / sourceRate.FPS(), float64(values[3]) / sourceRate.FPS(), int(channel)})
				}
			}
		}
		name := seq.text("name")
		if _, exists := sequences[name]; exists {
			return nil, fmt.Errorf("duplicate sequence name in import")
		}
		sequences[name] = items
	}
	return sequences, nil
}
func NativeAuditImportCommand(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("audit-import needs original XML and Premiere roundtrip XML")
	}
	f := flag.NewFlagSet("audit-import", flag.ContinueOnError)
	output := f.String("output", "import-audit.json", "audit output")
	if err := f.Parse(args[2:]); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected audit arguments")
	}
	return auditNativeRoundtrip(args[0], args[1], *output)
}
func auditNativeRoundtrip(original, imported, output string) error {
	expected, err := importSnapshot(original)
	if err != nil {
		return err
	}
	actual, err := importSnapshot(imported)
	if err != nil {
		return err
	}
	errors, differences := []map[string]any{}, []map[string]any{}
	checked := 0
	names := []string{}
	for name := range actual {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		items := actual[name]
		old, ok := expected[name]
		if !ok {
			errors = append(errors, map[string]any{"sequence": name, "reason": "unexpected_sequence"})
			continue
		}
		remaining := append([]importItem(nil), old...)
		for _, item := range items {
			best := -1
			distance := math.Inf(1)
			for i, other := range remaining {
				if item.Kind != other.Kind || item.Source != other.Source || item.Channel != other.Channel {
					continue
				}
				d := math.Abs(item.Start-other.Start) + math.Abs(item.End-other.End)
				if d < distance {
					distance = d
					best = i
				}
			}
			if best < 0 {
				errors = append(errors, map[string]any{"clip": item.Name, "reason": "unexpected_or_relinked_source_or_channel"})
				continue
			}
			other := remaining[best]
			remaining = append(remaining[:best], remaining[best+1:]...)
			checked++
			delta := map[string]float64{"start": item.Start - other.Start, "end": item.End - other.End, "source_in": item.In - other.In, "source_out": item.Out - other.Out}
			different, bad := false, item.In < 0 || item.Out <= item.In || item.End <= item.Start
			tol := 1/math.Min(item.Rate, other.Rate) + 1e-6
			for _, v := range delta {
				different = different || math.Abs(v) > 1e-8
				bad = bad || math.Abs(v) > tol
			}
			if different {
				differences = append(differences, map[string]any{"clip": item.Name, "kind": item.Kind, "delta_seconds": delta})
			}
			if bad {
				errors = append(errors, map[string]any{"clip": item.Name, "kind": item.Kind, "reason": "source_or_timeline_range_changed", "expected": other, "imported": item, "delta_seconds": delta})
			}
		}
		for _, item := range remaining {
			errors = append(errors, map[string]any{"clip": item.Name, "kind": item.Kind, "reason": "missing_timeline_item"})
		}
	}
	if len(actual) == 0 {
		errors = append(errors, map[string]any{"reason": "no_imported_sequences"})
	}
	unchecked := []string{}
	for name := range expected {
		if _, ok := actual[name]; !ok {
			unchecked = append(unchecked, name)
		}
	}
	sort.Strings(unchecked)
	status := "PASS_CHECKED_SEQUENCES"
	if len(errors) > 0 {
		status = "FAIL_IMPORT_ROUNDTRIP"
	}
	report := map[string]any{"status": status, "checked_clipitems": checked, "checked_sequences": names, "unchecked_sequences": unchecked, "errors": errors, "frame_rounding_differences": differences, "acoustic_accuracy": "not_assessed_by_this_import_check"}
	if err = writeJSON(output, report); err != nil {
		return err
	}
	fmt.Printf("IMPORT AUDIT %s: %d items, %d errors\n", status, checked, len(errors))
	if len(errors) > 0 {
		return fmt.Errorf("Premiere import changed source or timeline ranges")
	}
	return nil
}
