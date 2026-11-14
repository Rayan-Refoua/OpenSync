package autosync

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type repeatedStrings []string

func (s *repeatedStrings) String() string     { return strings.Join(*s, ",") }
func (s *repeatedStrings) Set(v string) error { *s = append(*s, v); return nil }
func NativeSyncCommand(ctx context.Context, args []string) error {
	o := NativeSyncOptions{}
	filtered := []string{}
	for i := 0; i < len(args); i++ {
		if args[i] == "--files" || args[i] == "--folders" {
			kind := args[i]
			first := i + 1
			for i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				i++
				if kind == "--folders" {
					o.Folders = append(o.Folders, args[i])
				} else {
					o.Files = append(o.Files, args[i])
				}
			}
			if i+1 == first {
				return fmt.Errorf("%s needs paths", kind)
			}
		} else {
			filtered = append(filtered, args[i])
		}
	}
	f := flag.NewFlagSet("sync", flag.ContinueOnError)
	f.Usage = func() {
		fmt.Fprintln(f.Output(), "Usage: opensync sync --files <files...> | --folders <folders...> | --file-list <list> --output <project folder>\nFiles and folders may be combined; folder scans include subdirectories. Caching is OFF unless explicitly requested.")
		f.PrintDefaults()
	}
	var sources repeatedStrings
	f.StringVar(&o.Input, "input", "", "recursively scan this input root (optionally restrict with --source)")
	f.Var(&sources, "source", "source folder (repeatable)")
	f.StringVar(&o.FileList, "file-list", "", "UTF-8 text or JSON list of media paths")
	f.StringVar(&o.Output, "output", "OpenSync-Output", "project output directory")
	f.StringVar(&o.CacheIndex, "cache-index", "", "existing guarded PCM cache index")
	f.StringVar(&o.Name, "name", "OpenSync", "project name")
	f.StringVar(&o.Unsynced, "unsynced", "append", "append or separate-sequence")
	f.StringVar(&o.Stage, "stage", "all", "cache, discover, verify, or all")
	f.Float64Var(&o.MaxResidualMS, "max-residual-ms", 150, "maximum export presentation residual")
	f.BoolVar(&o.CacheEnabled, "cache", false, "explicitly retain analysis cache for reuse; never copy originals")
	f.BoolVar(&o.Resume, "resume", false, "resume compatible cached work")
	f.BoolVar(&o.IncludeProvisional, "include-provisional", false, "include marked review candidates")
	if err := f.Parse(filtered); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if f.NArg() > 0 {
		return fmt.Errorf("unexpected sync arguments")
	}
	o.Sources = sources
	return NativeSync(ctx, o)
}
func NativeSync(ctx context.Context, o NativeSyncOptions) error {
	fmt.Printf("OpenSync %s\n", BuildVersion)
	// These switches are explicit requests to persist/reuse analysis.
	o.CacheEnabled = o.CacheEnabled || o.CacheIndex != "" || o.Resume || o.Stage == "cache"
	if !o.CacheEnabled {
		if o.Stage != "all" {
			return fmt.Errorf("partial analysis stages require explicit --cache; normal sync runs all stages without caching")
		}
		workspace := &nativeWorkspace{data: map[string][]byte{}, limit: 512 << 20, audio: newDirectAudio()}
		if o.ManagedMemoryMiB > 0 {
			budget := int64(o.ManagedMemoryMiB) << 20
			workspace.limit = min(workspace.limit, max(int64(16<<20), budget/8))
			workspace.audio.limit = min(workspace.audio.limit, max(int64(32<<20), budget/2))
		}
		ctx = context.WithValue(ctx, workspaceKey{}, workspace)
		defer memoryWorkspace(ctx).audio.Close()
		fmt.Printf("MEMORY audio working-set limit %d MiB; disk cache disabled\n", memoryWorkspace(ctx).audio.limit>>20)
	}
	if o.MaxResidualMS < 1 || o.MaxResidualMS > 150 {
		return fmt.Errorf("residual limit must be 1..150ms")
	}
	if o.Unsynced != "append" && o.Unsynced != "separate-sequence" {
		return fmt.Errorf("invalid unresolved policy")
	}
	if o.Stage != "cache" && o.Stage != "discover" && o.Stage != "verify" && o.Stage != "all" {
		return fmt.Errorf("invalid stage")
	}
	out, err := filepath.Abs(o.Output)
	if err != nil {
		return err
	}
	o.Output = out
	files, excluded, err := nativeInputFiles(o)
	if err != nil {
		return err
	}
	config := map[string]any{"version": "opensync-autonomous-v1", "files": append([]string(nil), files...), "max_residual_ms": o.MaxResidualMS}
	configPath := filepath.Join(out, "run-config.json")
	if data, err := workspaceRead(ctx, configPath); err == nil {
		if !o.Resume {
			return fmt.Errorf("existing analysis requires --resume; choose a new output directory for a fresh run")
		}
		previous, err := rawObject(data)
		if err != nil {
			return err
		}
		if number(previous["max_residual_ms"]) != o.MaxResidualMS || previous["version"] != "opensync-autonomous-v1" {
			return fmt.Errorf("cannot resume with changed DSP settings")
		}
		data, err = workspaceRead(ctx, filepath.Join(out, "inventory.json"))
		if err == nil {
			var inventory struct {
				Records  []MediaRecord
				Excluded []map[string]any
			}
			if err = json.Unmarshal(data, &inventory); err != nil {
				return err
			}
			old := []string{}
			if selected, ok := previous["files"].([]any); ok {
				for _, v := range selected {
					p, ok := v.(string)
					if !ok {
						return fmt.Errorf("invalid saved input selection")
					}
					old = append(old, strings.ToLower(p))
				}
			} else {
				return fmt.Errorf("saved run has no source selection")
			}
			current := []string{}
			for _, p := range files {
				current = append(current, strings.ToLower(p))
			}
			sort.Strings(old)
			sort.Strings(current)
			if strings.Join(old, "\x00") != strings.Join(current, "\x00") {
				return fmt.Errorf("cannot resume with changed source selection")
			}
			// Evidence uses inventory indices. Preserve the saved order even
			// when the caller lists the same files in a different order.
			files = files[:0]
			excluded = inventory.Excluded
			for _, r := range inventory.Records {
				files = append(files, r.Source)
			}
		} else if !os.IsNotExist(err) {
			return err
		} else {
			old, _ := json.Marshal(previous)
			want, _ := json.Marshal(config)
			if string(old) != string(want) {
				return fmt.Errorf("incomplete run selection differs")
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	} else if o.Resume {
		return fmt.Errorf("no saved analysis to resume; omit --resume for a fresh run")
	} else if entries, e := os.ReadDir(out); e == nil && len(entries) > 0 {
		return fmt.Errorf("output contains unrelated files; choose an empty directory")
	}
	if err = os.MkdirAll(out, 0755); err != nil {
		return err
	}
	if err = workspaceJSON(ctx, configPath, config); err != nil {
		return err
	}
	for _, folder := range []string{"features", "features-native", "evidence"} {
		if err = workspaceMkdir(ctx, filepath.Join(out, folder), 0755); err != nil {
			return err
		}
	}
	progress := startNativeProgress(ctx)
	if w := memoryWorkspace(ctx); w != nil {
		progress.monitor(func() string {
			return fmt.Sprintf(" | fingerprint buckets %d/%d active, %d done", w.indexActive.Load(), w.indexWorkers.Load(), w.indexCompleted.Load())
		})
	}
	defer progress.close()
	progress.set("Reading media metadata")
	start := time.Now()
	stages := map[string]float64{}
	stage := time.Now()
	records, err := nativePrepare(ctx, o, files, excluded)
	if err != nil {
		return err
	}
	stages["prepare_seconds"] = time.Since(stage).Seconds()
	observe(ctx, ProgressEvent{Kind: "inventory", Records: records, Total: len(records)})
	if o.Stage == "cache" {
		return writeJSON(filepath.Join(out, "native-run-profile.json"), map[string]any{"stages": stages, "total_seconds": time.Since(start).Seconds()})
	}
	stage = time.Now()
	discovery := filepath.Join(out, "discovery.json")
	var pairs []NativePair
	if data, err := workspaceRead(ctx, discovery); err == nil {
		var report struct{ Pairs []NativePair }
		if err = json.Unmarshal(data, &report); err != nil {
			return err
		}
		pairs = report.Pairs
	} else if !os.IsNotExist(err) {
		return err
	} else {
		progress.set("Streaming audio and extracting fingerprints")
		hashes, metrics, err := NativeFingerprintIndex(ctx, records, out, false)
		if err != nil {
			return err
		}
		stages["fingerprint_seconds"] = time.Since(stage).Seconds()
		if err = workspaceJSON(ctx, filepath.Join(out, "native-fingerprint-profile.json"), metrics); err != nil {
			return err
		}
		stage = time.Now()
		progress.set("Discovering candidate pairs")
		pairs, err = discoverWithNumericalAlternatives(ctx, records, hashes, out)
		if err != nil {
			return err
		}
		if err = workspaceJSON(ctx, discovery, map[string]any{"version": "opensync-autonomous-v1", "pairs": pairs, "status": "discovery_only", "implementation": "native-go-v2"}); err != nil {
			return err
		}
	}
	stages["candidate_discovery_seconds"] = time.Since(stage).Seconds()
	var analyzer *NativeAnalyzer
	profile := func() error {
		report := map[string]any{"build": BuildVersion, "stages": stages, "total_seconds": time.Since(start).Seconds(), "recordings": len(records), "python_runtime": false}
		if w := memoryWorkspace(ctx); w != nil {
			fmt.Printf("AUDIO additional window decodes %d; decoder starts %d\n", w.audio.misses.Load(), w.audio.starts.Load())
			report["additional_audio_window_decodes"] = w.audio.misses.Load()
			report["additional_decoder_starts"] = w.audio.starts.Load()
			report["direct_pcm_window_reads"] = w.audio.nativeReads.Load()
			report["fingerprint_pool"] = map[string]any{"limit": w.indexWorkers.Load(), "peak_active_tasks": w.indexPeak.Load(), "completed_tasks": w.indexCompleted.Load()}
		}
		if analyzer != nil && analyzer.compute != nil {
			p := analyzer.compute
			report["measurement_and_feature_pool"] = map[string]any{"limit": p.limit, "peak_active_tasks": p.peak.Load(), "completed_tasks": p.completed.Load()}
		}
		return writeJSON(filepath.Join(out, "native-run-profile.json"), report)
	}
	if o.Stage == "discover" {
		return profile()
	}
	analyzer = NewNativeAnalyzer(records)
	analyzer.ctx = ctx
	progress.monitor(func() string {
		s := ""
		if p := analyzer.computeStats.Load(); p != nil {
			s = fmt.Sprintf(" | buckets %d/%d active, %d ready, %d done", p.active.Load(), p.limit, len(p.jobs), p.completed.Load())
		}
		if w := memoryWorkspace(ctx); w != nil {
			s += fmt.Sprintf(" | audio %d reading, %d waiting", w.audio.reading.Load(), w.audio.waiting.Load())
		}
		return s
	})
	defer analyzer.Close()
	stage = time.Now()
	progress.set("Verifying candidate pairs")
	edges, err := analyzer.verify(ctx, pairs, out, "evidence-v2")
	if err != nil {
		return err
	}
	stages["initial_verification_seconds"] = time.Since(stage).Seconds()
	observe(ctx, ProgressEvent{Kind: "evidence", Stage: "Initial acoustic support", Completed: len(edges), Total: len(pairs)})
	stage = time.Now()
	progress.set("Searching unresolved recordings")
	recovery, err := analyzer.recover(ctx, edges, out)
	if err != nil {
		return err
	}
	stages["recovery_discovery_seconds"] = time.Since(stage).Seconds()
	initial := map[[2]int]NativePair{}
	for _, p := range pairs {
		initial[[2]int{p.Reference, p.Query}] = p
	}
	for i := range recovery {
		p := &recovery[i]
		for _, c := range initial[[2]int{p.Reference, p.Query}].Candidates {
			distinct := true
			for _, r := range p.Candidates {
				if math.Abs(c.Offset-r.Offset) < .2 {
					distinct = false
					break
				}
			}
			if distinct {
				p.Candidates = append(p.Candidates, c)
				p.raw = nil
			}
		}
	}
	stage = time.Now()
	progress.set("Verifying recovery candidates")
	recovered, err := analyzer.verify(ctx, recovery, out, "evidence-recovery-v2")
	if err != nil {
		return err
	}
	replaced := map[[2]int]bool{}
	for _, p := range recovery {
		replaced[[2]int{p.Reference, p.Query}] = true
	}
	combined := []*PairEvidence{}
	for _, e := range edges {
		if !replaced[[2]int{e.Reference, e.Query}] {
			combined = append(combined, e)
		}
	}
	edges = append(combined, recovered...)
	if err = workspaceJSON(ctx, filepath.Join(out, "supported-edges.json"), map[string]any{"version": "opensync-autonomous-v1", "edges": edges, "recovery": "envelope-recovery-v1"}); err != nil {
		return err
	}
	validated, err := analyzer.validateEdges(edges, out, "initial-")
	if err != nil {
		return err
	}
	accepted := map[[2]int]bool{}
	for _, e := range validated {
		accepted[[2]int{e.Reference, e.Query}] = true
	}
	retry := []NativePair{}
	for _, e := range edges {
		if accepted[[2]int{e.Reference, e.Query}] {
			continue
		}
		p := NativePair{Reference: e.Reference, Query: e.Query}
		for _, c := range e.Candidates {
			candidate := c.NativeCandidate
			candidate.VerificationWindow = 12
			candidate.VerificationMaxWindows = 11
			p.Candidates = append(p.Candidates, candidate)
		}
		retry = append(retry, p)
	}
	refined, err := analyzer.verify(ctx, retry, out, "evidence-refined-v1")
	if err != nil {
		return err
	}
	combined = append(validated, refined...)
	if err = workspaceJSON(ctx, filepath.Join(out, "refined-supported-edges.json"), map[string]any{"version": "opensync-autonomous-v1", "edges": combined, "retry_pairs": len(retry), "thresholds_relaxed": false}); err != nil {
		return err
	}
	edges, err = analyzer.validateEdges(combined, out, "")
	if err != nil {
		return err
	}
	stages["recovery_and_validation_seconds"] = time.Since(stage).Seconds()
	stage = time.Now()
	progress.set("Evaluating timing models")
	profiles, err := analyzer.runProfiles(ctx, out)
	if err != nil {
		return err
	}
	preliminary, _ := selectNativeEdges(records, edges, profiles)
	layout, _, err := nativeSolve(records, preliminary)
	if err != nil {
		return err
	}
	progress.set("Distributed acoustic search")
	observe(ctx, ProgressEvent{Kind: "layout", Records: records, Layout: layout, Detail: "Preliminary timing; final graph validation pending"})
	distributed, err := runNativeDistributed(ctx, records, layout, recovery, out, analyzer.computeTasks)
	if err != nil {
		return err
	}
	progress.set("Refining timing models")
	profiles, err = analyzer.runProfiles(ctx, out)
	if err != nil {
		return err
	}
	stages["multilevel_and_distributed_seconds"] = time.Since(stage).Seconds()
	if o.Stage == "verify" {
		return profile()
	}
	stage = time.Now()
	for _, r := range records {
		if err = checkSource(r); err != nil {
			return err
		}
	}
	progress.set("Building project and Premiere XML")
	if err = nativeFinish(records, edges, profiles, distributed, o); err != nil {
		return err
	}
	stages["graph_and_export_seconds"] = time.Since(stage).Seconds()
	return profile()
}
func nativeFinish(records []MediaRecord, edges []*PairEvidence, profiles []NativeProfilePair, distributed *NativeDistributedReport, o NativeSyncOptions) error {
	edges, decisions := selectNativeEdges(records, edges, profiles)
	layout, graph, err := nativeSolve(records, edges)
	if err != nil {
		return err
	}
	graph["event_decisions"] = decisions
	graph["acoustic_relationships"] = nativeRelationshipMap(records, profiles, graph)
	graph["provisional_placements"] = nativeComparisonPlacements(records, layout, profiles, distributed)
	if distributed != nil {
		graph["candidate_discovery"] = distributed
	}
	hypotheses := []map[string]any{}
	for _, p := range profiles {
		h := []map[string]any{}
		for _, v := range p.Profiles {
			if v.Status != "validated_relation" {
				h = append(h, map[string]any{"prior": v.Prior, "status": v.Status, "audit_result": v.AuditResult})
			}
		}
		if len(h) > 0 {
			hypotheses = append(hypotheses, map[string]any{"reference": p.Reference, "query": p.Query, "hypotheses": h})
		}
	}
	graph["unresolved_timing_hypotheses"] = hypotheses
	for name, value := range map[string]any{"event-decisions.json": map[string]any{"decisions": decisions, "baseline_positions_used": false}, "acoustic-relationships.json": graph["acoustic_relationships"], "graph.json": graph} {
		if o.CacheEnabled {
			if err = writeJSON(filepath.Join(o.Output, name), value); err != nil {
				return err
			}
		}
	}
	records = append([]MediaRecord(nil), records...)
	for i, r := range records {
		if r.SourceTimecode == nil {
			records[i].SourceTimecode = nativeSourceTimecode(r)
		}
	}
	path := filepath.Join(o.Output, "OpenSync.opensync")
	if err = saveNativeProject(path, o.Name, records, edges, layout, graph); err != nil {
		return err
	}
	return ExportNative(path, NativeExportOptions{Output: o.Output, Unsynced: o.Unsynced, MaxResidualMS: o.MaxResidualMS, IncludeProvisional: o.IncludeProvisional})
}
