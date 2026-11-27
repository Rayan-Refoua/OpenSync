package autosync

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
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
	o.FileTimePlacement = f.Bool("file-time-placement", true, "use file dates and order in chronological mode")
	f.Float64Var(&o.Analysis.Verification.MinWave, "min-wave", 0.1, "Minimum waveform correlation")
	f.Float64Var(&o.Analysis.Verification.MinFlux, "min-flux", 0.25, "Minimum spectral flux correlation")
	f.Float64Var(&o.Analysis.Verification.ControlRatio, "control-ratio", 1.5, "Minimum ratio over wrong-offset controls")
	f.Float64Var(&o.Analysis.Verification.ControlMargin, "control-margin", 0.035, "Minimum correlation lead over controls")
	f.Float64Var(&o.Analysis.Verification.AmbiguityRatio, "ambiguity-ratio", 0.8, "Runner-up confidence ratio that marks a pair ambiguous")
	f.Float64Var(&o.Analysis.Verification.EventToleranceMS, "event-tolerance-ms", 150, "Maximum separation for grouping competing event positions (ms)")
	var workers int
	f.StringVar(&o.Input, "input", "", "recursively scan this input root (optionally restrict with --source)")
	f.Var(&sources, "source", "source folder (repeatable)")
	f.StringVar(&o.FileList, "file-list", "", "UTF-8 text or JSON list of media paths")
	f.StringVar(&o.Output, "output", "OpenSync-Output", "project output directory")
	f.StringVar(&o.CacheIndex, "cache-index", "", "existing guarded PCM cache index")
	f.StringVar(&o.Name, "name", "OpenSync", "project name")
	f.StringVar(&o.Mode, "mode", "clips", "clips (acoustic placement) or chronological (also estimate bounded gaps from calibrated recording order)")
	f.StringVar(&o.Corrections, "corrections", "", "saved project/revision whose explicit rejected recordings and pairs must remain rejected")
	f.StringVar(&o.Unsynced, "unsynced", "append", "append, separate-sequence, order_only, disabled, remove, or local-tracks")
	f.StringVar(&o.Stage, "stage", "all", "sources, cache, discover, verify, or all")
	f.StringVar(&o.SourcePlanPath, "source-plan", "", "reviewed source-plan.json from a source-only run")
	f.StringVar(&o.ReferenceSource, "reference", "", "prefer this input recording as the timing reference of its connected group")
	f.BoolVar(&o.UseOriginalAudio, "original-audio", false, "use original audio media in the export without creating drift-corrected WAVs")
	f.Float64Var(&o.AudioEarlyMS, "audio-early-ms", 50, "allowed audio lead in milliseconds")
	f.Float64Var(&o.AudioLateMS, "audio-late-ms", 70, "allowed audio lag in milliseconds")
	f.Float64Var(&o.MaxResidualMS, "max-residual-ms", 150, "maximum export presentation residual")
	f.BoolVar(&o.Analysis.DisableSearchShortcuts, "no-search-shortcuts", false, "Disable chronology-first recovery skips and occupied-range exclusions")
	f.BoolVar(&o.Analysis.GuideRecorderOrder, "guide-recorder-order", false, "use tightly bracketed recording order as an acoustic search hint in chronological mode")
	f.StringVar(&o.Analysis.Recovery, "recovery", "balanced", "distributed recovery: off, quick, balanced, deep, or forensic; verification rules stay the same")
	f.StringVar(&o.Analysis.Discovery, "discovery", "", "deprecated compatibility option; the engine always searches full-resolution landmarks")
	f.StringVar(&o.Analysis.TimingProfiles, "timing-profiles", "auto", "auto, dense, or adaptive; auto uses sparse timing only on costly projects with independently supported long overlaps")
	f.BoolVar(&o.Analysis.GlobalTiming, "global-timing", false, "trial guarded global affine timing when holdouts improve")
	f.StringVar(&o.Analysis.ReadMode, "read-mode", "auto", "auto uses direct MP4/MOV reads on confirmed SSDs; buffered overrides them")
	f.StringVar(&o.Analysis.Readers, "readers", "auto", "fingerprint readers: adaptive (auto), serial, or parallel (up to two; disk scheduling still applies)")
	f.Float64Var(&o.Analysis.LongReferenceHours, "long-reference-hours", 0, "total long-reference search budget in hours; 0 uses the recovery preset")
	f.Float64Var(&o.Analysis.RecoveryReferenceHours, "recovery-reference-hours", 0, "maximum total reference hours scheduled for distributed recovery (0 adds no project cap)")
	f.IntVar(&workers, "workers", 0, "Go CPU concurrency ceiling (0 uses the runtime default)")
	f.IntVar(&o.ManagedMemoryMiB, "memory-mib", 0, "in-memory analysis budget in MiB (0 uses automatic sizing)")
	f.BoolVar(&o.OmitProjectMarker, "omit-project-marker", false, "Omit the OpenSync project summary marker at time zero in Premiere XML")
	f.BoolVar(&o.KeepTrackOrder, "keep-track-order", false, "Keep OpenSync track order in Premiere XML (default reverses tracks)")
	f.StringVar(&o.TrackLayout, "track-layout", "family", "Premiere track grouping: family or folder")
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
	if workers < 0 || workers > 1024 {
		return fmt.Errorf("workers must be 0..1024")
	}
	if workers > 0 {
		previous := runtime.GOMAXPROCS(workers)
		defer runtime.GOMAXPROCS(previous)
	}
	o.Sources = sources
	return NativeSync(ctx, o)
}
func NativeSync(ctx context.Context, o NativeSyncOptions) error {
	if err := o.Analysis.Normalize(); err != nil {
		return err
	}
	if o.ManagedMemoryMiB != 0 && (o.ManagedMemoryMiB < 256 || o.ManagedMemoryMiB > 131072) {
		return fmt.Errorf("memory-mib must be 0 or 256..131072")
	}
	if !validTrackLayout(o.TrackLayout) {
		return fmt.Errorf("track-layout must be family or folder")
	}
	ctx = context.WithValue(ctx, analysisSettingsKey{}, o.Analysis)
	if o.Mode == "" {
		o.Mode = "clips"
	}
	if o.Mode != "clips" && o.Mode != "chronological" {
		return fmt.Errorf("mode must be clips or chronological")
	}
	fmt.Printf("OpenSync %s\n", BuildVersion)
	for _, tolerance := range []float64{o.AudioEarlyMS, o.AudioLateMS} {
		if tolerance < 0 || math.IsNaN(tolerance) || math.IsInf(tolerance, 0) {
			return fmt.Errorf("invalid audio drift tolerance")
		}
	}
	fmt.Printf("POLICY recovery=%s timing-profiles=%s read-mode=%s readers=%s\n", o.Analysis.Recovery, o.Analysis.TimingProfiles, o.Analysis.ReadMode, o.Analysis.Readers)
	// These switches are explicit requests to persist/reuse analysis.
	o.CacheEnabled = o.CacheEnabled || o.CacheIndex != "" || o.Resume || o.Stage == "cache"
	if !o.CacheEnabled {
		if o.Stage != "all" && o.Stage != "sources" {
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
	if !(o.MaxResidualMS >= 1 && o.MaxResidualMS <= 150) {
		return fmt.Errorf("residual limit must be 1..150ms")
	}
	if !validUnmatchedPolicy(o.Unsynced) {
		return fmt.Errorf("invalid unresolved policy")
	}
	if o.Stage != "sources" && o.Stage != "cache" && o.Stage != "discover" && o.Stage != "verify" && o.Stage != "all" {
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
	var records []MediaRecord
	reviewBeforeCache := o.CacheEnabled && o.Stage != "sources" && (o.SourcePlanPath != "" || o.ReviewSources != nil)
	if o.Stage == "sources" || reviewBeforeCache {
		records, err = nativePrepareDirect(ctx, o, files, excluded)
	} else {
		records, err = nativePrepare(ctx, o, files, excluded)
	}
	if err != nil {
		return err
	}
	ledger, err := correctionsForRecords(o.Corrections, records)
	if err != nil {
		return err
	}
	if o.Corrections != "" {
		prior, err := LoadNativeProject(o.Corrections)
		if err != nil {
			return err
		}
		byID := make(map[string]MediaRecord, len(prior.Data.Records))
		for _, record := range prior.Data.Records {
			byID[record.ID] = record
		}
		for i, record := range records {
			if prior, ok := byID[record.ID]; ok && prior.SourceBytes == record.SourceBytes && prior.SourceMtime == record.SourceMtime {
				records[i].SourceAssignment = prior.SourceAssignment
			}
		}
	}
	proposed := sourceInventory(records)
	if o.SourcePlanPath != "" || o.ReviewSources != nil {
		var reviewed SourcePlan
		if o.SourcePlanPath != "" {
			data, err := os.ReadFile(o.SourcePlanPath)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(data, &reviewed); err != nil {
				return fmt.Errorf("read source plan: %w", err)
			}
		} else {
			reviewed, err = o.ReviewSources(ctx, proposed)
			if err != nil {
				return err
			}
		}
		if err := applySourcePlan(records, proposed, reviewed); err != nil {
			return err
		}
	}
	if reviewBeforeCache {
		inspected := sourceInventory(records)
		progress.set("Preparing audio cache")
		records, err = nativePrepare(ctx, o, files, excluded)
		if err != nil {
			return err
		}
		if err := applySourcePlan(records, sourceInventory(records), inspected); err != nil {
			return err
		}
	}
	stages["prepare_seconds"] = time.Since(stage).Seconds()
	for _, warning := range proposed.Warnings {
		fmt.Println("SOURCE:", warning)
		observe(ctx, ProgressEvent{Kind: "source", Detail: warning})
	}
	audit := fmt.Sprintf("Source occupancy audit: %d potentially avoidable comparisons based on file timestamps.", sourceComparisonAudit(records, o.Mode))
	fmt.Println(audit)
	observe(ctx, ProgressEvent{Kind: "source", Detail: audit})
	if o.Stage == "sources" {
		return writeJSON(filepath.Join(out, "source-plan.json"), sourceInventory(records))
	}
	o.reviewLedger = &ledger
	observe(ctx, ProgressEvent{Kind: "inventory", Records: records, Total: len(records)})
	if o.ReferenceSource != "" && len(preferredReference(records, o.ReferenceSource)) == 0 {
		detail := "Preferred reference is outside this input set. Using automatic reference selection."
		fmt.Println(detail)
		observe(ctx, ProgressEvent{Kind: "source", Detail: detail})
	}
	if o.Stage == "cache" {
		return writeJSON(filepath.Join(out, "native-run-profile.json"), map[string]any{"stages": stages, "total_seconds": time.Since(start).Seconds()})
	}
	stage = time.Now()
	discovery := filepath.Join(out, "discovery.json")
	var pairs []NativePair
	var fingerprintCount int
	discoveryReused := false
	if data, err := workspaceRead(ctx, discovery); err == nil {
		var report struct{ Pairs []NativePair }
		if err = json.Unmarshal(data, &report); err != nil {
			return err
		}
		pairs = report.Pairs
		discoveryReused = true
	} else if !os.IsNotExist(err) {
		return err
	} else {
		progress.set("Streaming audio and extracting fingerprints")
		hashes, metrics, err := NativeFingerprintIndex(ctx, records, out, false)
		if err != nil {
			return err
		}
		for _, recordingHashes := range hashes {
			fingerprintCount += len(recordingHashes)
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
		report := map[string]any{"version": "opensync-autonomous-v1", "pairs": pairs, "status": "discovery_only", "implementation": "native-go-v2"}
		if err = workspaceJSON(ctx, discovery, report); err != nil {
			return err
		}
	}
	stages["candidate_discovery_seconds"] = time.Since(stage).Seconds()
	var analyzer *NativeAnalyzer
	profile := func() error {
		report := map[string]any{"analysis_policy": o.Analysis, "managed_memory_mib": o.ManagedMemoryMiB, "cpu_concurrency": runtime.GOMAXPROCS(0), "build": BuildVersion, "stages": stages, "total_seconds": time.Since(start).Seconds(), "recordings": len(records), "python_runtime": false}
		var audioSeconds float64
		for _, r := range records {
			if len(r.Audio) > 0 {
				audioSeconds += r.Duration
			}
		}
		hypotheses := 0
		for _, pair := range pairs {
			hypotheses += len(pair.Candidates)
		}
		report["discovery_work"] = map[string]any{"audio_seconds": audioSeconds, "pairs": len(pairs), "hypotheses": hypotheses, "reused": discoveryReused}
		if !discoveryReused {
			report["primary_fingerprints"] = fingerprintCount
		}
		if analyzer != nil {
			report["verification_work"] = map[string]any{
				"evaluated_hypotheses": analyzer.verificationCandidates.Load(), "reused_hypotheses": analyzer.reusedVerificationCandidates.Load(),
				"prepared_waveform_comparisons": analyzer.preparedWaveformCorrelations.Load(), "prepared_negative_control_comparisons": analyzer.negativeControlCorrelations.Load(),
				"timing_training_windows": analyzer.profileWindows[0].Load(), "timing_selection_windows": analyzer.profileWindows[1].Load(), "timing_audit_windows": analyzer.profileWindows[2].Load(),
				"timing_sparse_completed": analyzer.sparseProfilesCompleted.Load(), "timing_sparse_escalated": analyzer.sparseProfilesEscalated.Load(),
			}
		}
		if w := memoryWorkspace(ctx); w != nil {
			fmt.Printf("AUDIO additional window decodes %d; decoder starts %d\n", w.audio.misses.Load(), w.audio.starts.Load())
			report["additional_audio_window_decodes"] = w.audio.misses.Load()
			report["additional_decoder_starts"] = w.audio.starts.Load()
			report["direct_pcm_window_reads"] = w.audio.nativeReads.Load()
			report["shared_pcm_block_requests"] = w.audio.sharedReads.Load()
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
	if o.Mode == "chronological" && (o.FileTimePlacement == nil || *o.FileTimePlacement || o.Analysis.GuideRecorderOrder) &&
		o.Analysis.Recovery != "off" && !o.Analysis.DisableSearchShortcuts && !o.Analysis.Verification.custom() &&
		len(ledger.Pairs)+len(ledger.Recordings) == 0 {
		shortcutStarted := time.Now()
		early, err := analyzer.chronologyFirst(ctx, edges, pairs, out, progress)
		if err != nil {
			return err
		}
		edges = append(edges, early...)
		stages["chronology_first_seconds"] = time.Since(shortcutStarted).Seconds()
	}
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
	recovered, err := analyzer.verify(ctx, recovery, out, "evidence-recovery-v2", initial)
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
	profiles, err := analyzer.runProfiles(ctx, out, edges)
	if err != nil {
		return err
	}
	stages["initial_timing_profiles_seconds"] = time.Since(stage).Seconds()
	graphStarted := time.Now()
	progress.set("Solving independent timing anchors")
	preliminary, decisions := selectNativeEdges(records, edges, profiles, o.Analysis.Verification)
	layout, _, err := nativeSolve(records, preliminary, preferredReference(records, o.ReferenceSource)...)
	if err != nil {
		return err
	}
	if o.Mode == "chronological" && (o.FileTimePlacement == nil || *o.FileTimePlacement) && o.Analysis.Recovery != "off" && len(ledger.Pairs) == 0 && len(ledger.Recordings) == 0 {
		blocked := map[int]bool{}
		for _, d := range decisions {
			if d["status"] == "unresolved_competing_events" {
				blocked[int(number(d["recording"]))] = true
			}
		}
		candidates := dateGuidedPairs(records, layout, blocked, append(append([]NativePair(nil), pairs...), recovery...))
		if len(candidates) > 0 {
			found, err := analyzer.verify(ctx, candidates, out, "date-guided-v1")
			if err != nil {
				return err
			}
			nearby := found[:0]
			for _, e := range found {
				if e.Selected.Method == "calibrated_chronology_audio_search" {
					nearby = append(nearby, e)
				}
			}
			checked, err := analyzer.validateEdges(nearby, out, "date-guided-")
			if err != nil {
				return err
			}
			fmt.Printf("CHRONOLOGY searched %d predicted overlaps; %d passed acoustic holdout\n", len(candidates), len(checked))
			if len(checked) > 0 {
				edges = append(edges, checked...)
				preliminary, decisions = selectNativeEdges(records, edges, profiles, o.Analysis.Verification)
				layout, _, err = nativeSolve(records, preliminary, preferredReference(records, o.ReferenceSource)...)
				if err != nil {
					return err
				}
			}
		}
	}
	stages["preliminary_anchor_graph_seconds"] = time.Since(graphStarted).Seconds()
	progress.set("Distributed acoustic search")
	observe(ctx, ProgressEvent{Kind: "layout", Records: records, Layout: layout, Detail: "Preliminary timing; final graph validation pending"})
	distributed, err := runNativeDistributed(ctx, records, layout, recovery, out, analyzer.computeTasks, profiles, buildOccupiedSearch(records, layout, preliminary, decisions, o))
	if err != nil {
		return err
	}
	progress.set("Refining timing models")
	profiles, err = analyzer.runProfiles(ctx, out, edges)
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
	if err = nativeFinish(records, edges, profiles, distributed, o, analyzer); err != nil {
		return err
	}
	stages["graph_and_export_seconds"] = time.Since(stage).Seconds()
	return profile()
}
func nativeFinish(records []MediaRecord, edges []*PairEvidence, profiles []NativeProfilePair, distributed *NativeDistributedReport, o NativeSyncOptions, analyzers ...*NativeAnalyzer) error {
	var ledger ReviewLedger
	var err error
	if o.reviewLedger != nil {
		ledger = *o.reviewLedger
	} else {
		ledger, err = correctionsForRecords(o.Corrections, records)
	}
	if err != nil {
		return err
	}
	edges, err = filterReviewedEdges(records, edges, ledger)
	if err != nil {
		return err
	}
	blocked, rejected, _ := reviewBlocked(records, ledger)
	filtered := make([]NativeProfilePair, 0, len(profiles))
	for _, p := range profiles {
		if !blocked[p.Reference] && !blocked[p.Query] && !rejected[orderedReviewPair(records[p.Reference].ID, records[p.Query].ID)] {
			filtered = append(filtered, p)
		}
	}
	profiles = filtered
	edges, decisions := selectNativeEdges(records, edges, profiles, o.Analysis.Verification)
	var predictions map[int]recorderPrediction
	var conflicts []map[string]any
	if o.Mode == "chronological" && (o.FileTimePlacement == nil || *o.FileTimePlacement) && len(analyzers) > 0 && len(ledger.Pairs)+len(ledger.Recordings) == 0 {
		predictions, err = timestampRecovery(analyzers[0], records, edges)
		if err != nil {
			return err
		}
		kept := make([]*PairEvidence, 0, len(edges))
		for _, e := range edges {
			q, qok := predictions[e.Query]
			r, rok := predictions[e.Reference]
			if qok && e.Reference == q.ref {
				r = recorderPrediction{ref: q.ref}
				rok = true
			}
			if qok && rok && q.ref == r.ref && (q.basis != "filename_clock_consensus" || (e.Reference != q.ref && r.basis != "filename_clock_consensus")) && math.Abs(q.offset-r.offset-e.Selected.Offset) > 3 {
				conflicts = append(conflicts, map[string]any{"reference": e.Reference, "query": e.Query, "observed": e.Selected.Offset, "predicted": q.offset - r.offset})
				continue
			}
			kept = append(kept, e)
		}
		edges = kept
	}
	preferred := preferredReference(records, o.ReferenceSource)
	layout, graph, err := nativeSolve(records, edges, preferred...)
	if err != nil {
		return err
	}
	graph["event_decisions"] = decisions
	if len(preferred) > 0 {
		graph["preferred_reference_recording"] = preferred[0]
		graph["clock_policy"] = "preferred_recording_then_video"
	}
	if active, _ := graph["active_edges"].([]int); len(active) > 0 {
		if report := auditGlobalAffine(records, layout, edges, active); len(report) > 0 {
			if o.Analysis.GlobalTiming {
				applyGlobalAffine(layout, edges, graph, report)
			}
			graph["global_affine_audit"] = report
		}
	}
	graph["review_ledger"] = ledger
	mode := o.Mode
	if mode == "" {
		mode = "clips"
	}
	graph["placement_mode"] = mode
	graph["analysis_policy"] = o.Analysis
	graph["recording_continuity"] = continuityEvidence(records, layout)
	enabled := o.FileTimePlacement == nil || *o.FileTimePlacement
	graph["file_time_placement"] = enabled
	if mode == "chronological" && enabled {
		layout = applyChronology(records, layout, graph, analyzers...)
		existing, _ := graph["chronological_placements"].([]ChronologicalPlacement)
		byID := map[int]NativeLayout{}
		for _, m := range layout {
			byID[m.Recording] = m
		}
		var extra []ChronologicalPlacement
		for _, m := range layout {
			p, ok := predictions[m.Recording]
			ref := byID[p.ref]
			if !ok || ref.Component != 1 || m.ComponentSize != 1 || !strings.HasPrefix(m.Status, "unresolved") {
				continue
			}
			start := MapTime(ref.Knots, p.offset) - sourceOrigin(records[m.Recording])
			extra = append(extra, ChronologicalPlacement{m.Recording, start, 1.5, p.basis, p.anchors, "provisional_chronological_estimate"})
		}
		if len(extra) > 0 {
			layout = placeChronological(layout, records, graph, extra)
			graph["chronological_placements"] = append(existing, extra...)
		}
		graph["recorder_clock_conflicts"] = conflicts
	}
	graph["acoustic_relationships"] = nativeRelationshipMap(records, profiles, graph)
	comparisonDiscovery := distributed
	if distributed != nil && (len(blocked) != 0 || len(rejected) != 0) {
		filteredDiscovery := *distributed
		filteredDiscovery.Pairs = nil
		for _, pair := range distributed.Pairs {
			if !blocked[pair.Reference] && !blocked[pair.Query] && !rejected[orderedReviewPair(records[pair.Reference].ID, records[pair.Query].ID)] {
				filteredDiscovery.Pairs = append(filteredDiscovery.Pairs, pair)
			}
		}
		comparisonDiscovery = &filteredDiscovery
	}
	graph["provisional_placements"] = nativeComparisonPlacements(records, layout, profiles, comparisonDiscovery)
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
	return ExportNative(path, NativeExportOptions{TrackLayout: o.TrackLayout, KeepTrackOrder: o.KeepTrackOrder, OmitProjectMarker: o.OmitProjectMarker, AudioEarlyMS: o.AudioEarlyMS, AudioLateMS: o.AudioLateMS, Output: o.Output, Unsynced: o.Unsynced, MaxResidualMS: o.MaxResidualMS, IncludeProvisional: o.IncludeProvisional, UseOriginalAudio: o.UseOriginalAudio})
}

func preferredReference(records []MediaRecord, path string) []int {
	if path != "" {
		target, err := filepath.Abs(path)
		if err == nil {
			for i, r := range records {
				source := filepath.Clean(r.Source)
				if source == target || (runtime.GOOS == "windows" && strings.EqualFold(source, target)) {
					return []int{i}
				}
			}
		}
	}
	return nil
}
