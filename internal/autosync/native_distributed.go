package autosync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"opensync/internal/researchmatch"
	"os"

	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type NativeDistributedPair struct {
	Reference int `json:"reference"`
	Query     int `json:"query"`
	researchmatch.DistributedResult
	Digest string `json:"input_digest"`
}
type NativeDistributedReport struct {
	Version                string                  `json:"version"`
	Digest                 string                  `json:"input_digest"`
	Pairs                  []NativeDistributedPair `json:"pairs"`
	Deferred               []map[string]any        `json:"deferred"`
	SecondsBudget          int                     `json:"reference_seconds_budget_per_query"`
	PairBudget             int                     `json:"pair_budget_per_query"`
	VerificationCandidates int                     `json:"fine_verification_candidates_per_pair"`
	OriginalRead           bool                    `json:"original_media_read"`
	Channels               string                  `json:"channel_policy"`
	Status                 string                  `json:"status"`
	Baseline               bool                    `json:"baseline_positions_used"`
}

func distributedFeatures(ctx context.Context, source, path string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	tool, err := mediaTool("ffmpeg")
	if err != nil {
		return err
	}
	command := mediaCommandContext(ctx, tool, "-nostdin", "-v", "error", "-threads", "1", "-i", source, "-map", "0:a:0", "-vn", "-ar", "8000", "-ac", "1", "-f", "f32le", "-")
	var stderr strings.Builder
	command.Stderr = &stderr
	pipe, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	temp, err := os.Create(path + ".partial")
	if err != nil {
		return err
	}
	defer temp.Close()
	if err = command.Start(); err != nil {
		return err
	}
	if err = researchmatch.WriteDistributed(pipe, temp, 1); err != nil {
		command.Process.Kill()
		command.Wait()
		return err
	}
	if _, err = io.Copy(io.Discard, pipe); err != nil {
		command.Process.Kill()
		command.Wait()
		return err
	}
	if err = command.Wait(); err != nil {
		return fmt.Errorf("distributed decoding: %w: %s", err, stderr.String())
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}
func runNativeDistributed(ctx context.Context, records []MediaRecord, layout []NativeLayout, envelope []NativePair, out string, schedule func(int, func(int))) (*NativeDistributedReport, error) {
	identities := []any{}
	for _, r := range records {
		identities = append(identities, []any{r.ID, r.CacheSHA})
	}
	raw, _ := pythonJSON([]any{researchmatch.DistributedVersion, "established-reference-v5", identities})
	identity := shaBytes(raw)
	saved := filepath.Join(out, "distributed-discovery.json")
	if data, err := workspaceRead(ctx, saved); err == nil {
		var report NativeDistributedReport
		if err = json.Unmarshal(data, &report); err != nil {
			return nil, err
		}
		if report.Digest == identity {
			return &report, nil
		}
	}
	folder := filepath.Join(out, researchmatch.DistributedVersion)
	if err := workspaceMkdir(ctx, folder, 0755); err != nil {
		return nil, err
	}
	targets := []int{}
	established := map[int]bool{}
	for _, m := range layout {
		established[m.Recording] = !strings.HasPrefix(m.Status, "unresolved") && !strings.HasPrefix(m.Status, "provisional") && m.ComponentSize > 1
		d := records[m.Recording].Duration
		if strings.HasPrefix(m.Status, "unresolved") && d >= 1.2 && d <= 299 {
			targets = append(targets, m.Recording)
		}
	}
	strength := map[[2]int]float64{}
	for _, p := range envelope {
		v := 0.
		for _, c := range p.Candidates {
			v = max(v, c.EnvelopeScore)
		}
		strength[[2]int{p.Reference, p.Query}] = v
	}
	// Spend the shared extension budget on stronger long-reference hints
	// before falling back to filename/inventory order.
	priority := map[int]float64{}
	for pair, v := range strength {
		if established[pair[0]] && records[pair[0]].Duration > 3600 && records[pair[0]].Duration <= 9000 {
			priority[pair[1]] = max(priority[pair[1]], v)
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		a, b := targets[i], targets[j]
		if priority[a] != priority[b] {
			return priority[a] > priority[b]
		}
		return a < b
	})
	pairs := [][2]int{}
	deferred := []map[string]any{}
	needed := map[int]bool{}
	// Bound the extension separately from the ordinary per-query budget.
	extraSeconds := 540000.0
	for _, q := range targets {
		refs := []int{}
		for r := range records {
			if r != q && records[r].Audio != nil && records[r].Duration >= records[q].Duration {
				refs = append(refs, r)
			}
		}
		sort.Slice(refs, func(i, j int) bool {
			a, b := refs[i], refs[j]
			sa, sb := strength[[2]int{a, q}], strength[[2]int{b, q}]
			if sa != sb {
				return sa > sb
			}
			if established[a] != established[b] {
				return established[a]
			}
			return records[a].ID < records[b].ID
		})
		used, selected := 0., 0
		limit := 8
		if records[q].Duration > 60 {
			limit = 2
		}
		// Reserve one attempt on a long, established recorder. Previously
		// every reference longer than the ordinary budget was unreachable.
		anchors := map[int]bool{}
		for _, r := range refs {
			d := records[r].Duration
			if established[r] && d > 3600 && d <= 9000 && d <= extraSeconds {
				anchors[r] = true
				extraSeconds -= d
				pairs = append(pairs, [2]int{r, q})
				needed[r], needed[q] = true, true
				break
			}
		}
		// A medium query may belong to a different part of an event.
		// Give it one bounded attempt against a second long recorder.
		if records[q].Duration > 60 {
			for _, r := range refs {
				d := records[r].Duration
				if !anchors[r] && d > 3600 && d <= 9000 && d <= extraSeconds {
					anchors[r] = true
					extraSeconds -= d
					pairs = append(pairs, [2]int{r, q})
					needed[r], needed[q] = true, true
					break
				}
			}
		}
		for _, r := range refs {
			if anchors[r] || (records[q].Duration > 60 && !established[r]) {
				continue
			}
			if selected >= limit || used+records[r].Duration > 3600 {
				deferred = append(deferred, map[string]any{"reference": r, "query": q, "reason": "discovery_compute_budget"})
				continue
			}
			pairs = append(pairs, [2]int{r, q})
			needed[r] = true
			needed[q] = true
			used += records[r].Duration
			selected++
		}
	}
	ids := []int{}
	paths := map[int]string{}
	for i := range needed {
		ids = append(ids, i)
		paths[i] = filepath.Join(folder, records[i].CacheSHA+".features")
	}
	sort.Ints(ids)
	errs := make([]error, len(ids))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < min(4, nativeWorkers(len(ids))); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				i := ids[j]
				if workspaceExists(ctx, paths[i]) {
					continue
				}
				if ctx.Err() == nil {
					errs[j] = distributedFeaturesForRecord(ctx, records[i], paths[i])
				}
			}
		}()
	}
	for i := range ids {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	results := make([]NativeDistributedPair, len(pairs))
	errs = make([]error, len(pairs))
	jobs = make(chan int)
	for w := 0; w < min(8, nativeWorkers(len(pairs))); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					continue
				}
				p := pairs[i]
				digest := shaBytes([]byte(researchmatch.DistributedVersion + records[p[0]].CacheSHA + records[p[1]].CacheSHA))
				path := filepath.Join(folder, digest+".json")
				var result researchmatch.DistributedResult
				if data, err := workspaceRead(ctx, path); err == nil {
					errs[i] = json.Unmarshal(data, &result)
				} else {
					ref, err := openDistributedWorkspace(ctx, paths[p[0]])
					if err != nil {
						errs[i] = err
						continue
					}
					query, err := openDistributedWorkspace(ctx, paths[p[1]])
					if err != nil {
						ref.Close()
						errs[i] = err
						continue
					}
					result, errs[i] = researchmatch.SearchDistributedScheduled(ref, query, -1, -1, schedule)
					ref.Close()
					query.Close()
					if errs[i] == nil {
						errs[i] = workspaceJSON(ctx, path, result)
					}
				}
				results[i] = NativeDistributedPair{p[0], p[1], result, digest}
			}
		}()
	}
	for i := range pairs {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	report := &NativeDistributedReport{researchmatch.DistributedVersion, identity, results, deferred, 21600, 9, 3, false, "cached_mono_first_pass", "DISCOVERY_ONLY", false}
	if err := workspaceJSON(ctx, saved, report); err != nil {
		return nil, err
	}
	fmt.Printf("DISTRIBUTED: %d blind searches\n", len(results))
	return report, nil
}
