package autosync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"opensync/internal/researchmatch"
	"os"
	"os/exec"
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
	tool, err := mediaTool("ffmpeg")
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, tool, "-nostdin", "-v", "error", "-threads", "1", "-i", source, "-map", "0:a:0", "-vn", "-ar", "8000", "-ac", "1", "-f", "f32le", "-")
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
	raw, _ := pythonJSON([]any{researchmatch.DistributedVersion, identities})
	identity := shaBytes(raw)
	saved := filepath.Join(out, "distributed-discovery.json")
	if data, err := workspaceRead(ctx, saved); err == nil {
		var report NativeDistributedReport
		if err = json.Unmarshal(data, &report); err != nil {
			return nil, err
		}
		if report.Digest != identity {
			return nil, fmt.Errorf("stale distributed discovery")
		}
		return &report, nil
	}
	folder := filepath.Join(out, researchmatch.DistributedVersion)
	if err := workspaceMkdir(ctx, folder, 0755); err != nil {
		return nil, err
	}
	targets := []int{}
	for _, m := range layout {
		d := records[m.Recording].Duration
		if strings.HasPrefix(m.Status, "unresolved") && d >= 1.2 && d <= 60 {
			targets = append(targets, m.Recording)
		}
	}
	sort.Ints(targets)
	strength := map[[2]int]float64{}
	for _, p := range envelope {
		v := 0.
		for _, c := range p.Candidates {
			v = max(v, c.EnvelopeScore)
		}
		strength[[2]int{p.Reference, p.Query}] = v
	}
	pairs := [][2]int{}
	deferred := []map[string]any{}
	needed := map[int]bool{}
	for _, q := range targets {
		refs := []int{}
		for r := range records {
			if r != q && records[r].Duration >= records[q].Duration {
				refs = append(refs, r)
			}
		}
		sort.Slice(refs, func(i, j int) bool {
			a, b := refs[i], refs[j]
			sa, sb := strength[[2]int{a, q}], strength[[2]int{b, q}]
			if sa != sb {
				return sa > sb
			}
			return records[a].ID < records[b].ID
		})
		used, selected := 0., 0
		for _, r := range refs {
			if selected >= 8 || used+records[r].Duration > 3600 {
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
	report := &NativeDistributedReport{researchmatch.DistributedVersion, identity, results, deferred, 3600, 8, 3, false, "cached_mono_first_pass", "DISCOVERY_ONLY", false}
	if err := workspaceJSON(ctx, saved, report); err != nil {
		return nil, err
	}
	fmt.Printf("DISTRIBUTED: %d blind searches\n", len(results))
	return report, nil
}
