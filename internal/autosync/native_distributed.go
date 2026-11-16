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
	settings := analysisSettings(ctx)
	identities := []any{}
	for _, r := range records {
		identities = append(identities, []any{r.ID, r.CacheSHA})
	}
	raw, _ := pythonJSON([]any{researchmatch.DistributedVersion, "coverage-first-v7", settings.Recovery, settings.LongReferenceHours, identities})
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
	if settings.Recovery == "off" {
		report := &NativeDistributedReport{Version: researchmatch.DistributedVersion, Digest: identity, Status: "DISABLED_BY_USER"}
		fmt.Println("DISTRIBUTED: disabled by user")
		return report, workspaceJSON(ctx, saved, report)
	}
	pairBudget, mediumBudget, secondsBudget, longBudget, maxDuration := 8, 2, 3600., 540000., 299.
	switch settings.Recovery {
	case "quick":
		pairBudget, mediumBudget, secondsBudget, longBudget, maxDuration = 4, 0, 1800, 90000, 60
	case "deep":
		pairBudget, mediumBudget, secondsBudget, longBudget = 16, 4, 7200, 1080000
	}
	if settings.LongReferenceHours > 0 {
		longBudget = settings.LongReferenceHours * 3600
	}
	folder := filepath.Join(out, researchmatch.DistributedVersion)
	if err := workspaceMkdir(ctx, folder, 0755); err != nil {
		return nil, err
	}
	targets := []int{}
	established := map[int]bool{}
	bridgeSize := map[int]int{}
	for _, m := range layout {
		established[m.Recording] = !strings.HasPrefix(m.Status, "unresolved") && !strings.HasPrefix(m.Status, "provisional") && m.ComponentSize > 1
		d := records[m.Recording].Duration
		if strings.HasPrefix(m.Status, "unresolved") && d >= 1.2 && d <= maxDuration {
			targets = append(targets, m.Recording)
			bridgeSize[m.Recording] = m.ComponentSize
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
		if (priority[a] > 0) != (priority[b] > 0) {
			return priority[a] > 0
		}
		if bridgeSize[a] != bridgeSize[b] {
			return bridgeSize[a] > bridgeSize[b]
		}
		if priority[a] != priority[b] {
			return priority[a] > priority[b]
		}
		return a < b
	})
	pairs := [][2]int{}
	deferred := []map[string]any{}
	needed := map[int]bool{}
	refsByQuery := make(map[int][]int, len(targets))
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
		refsByQuery[q] = refs
	}
	longPairs := planLongReferenceCoverage(records, targets, refsByQuery, established, longBudget)
	anchors := make(map[[2]int]bool, len(longPairs))
	for _, pair := range longPairs {
		pairs = append(pairs, pair)
		anchors[pair] = true
		needed[pair[0]], needed[pair[1]] = true, true
	}
	for _, q := range targets {
		refs := refsByQuery[q]
		used, selected := 0., 0
		limit := pairBudget
		if records[q].Duration > 60 {
			limit = mediumBudget
		}
		for _, r := range refs {
			if anchors[[2]int{r, q}] || (records[q].Duration > 60 && !established[r]) {
				continue
			}
			if selected >= limit || used+records[r].Duration > secondsBudget {
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
	report := &NativeDistributedReport{researchmatch.DistributedVersion, identity, results, deferred, int(secondsBudget) + 18000, max(pairBudget+1, mediumBudget+2), 3, false, "cached_mono_first_pass", "DISCOVERY_ONLY", false}
	if err := workspaceJSON(ctx, saved, report); err != nil {
		return nil, err
	}
	fmt.Printf("DISTRIBUTED: %d blind searches\n", len(results))
	return report, nil
}

// Exhaust first useful long-anchor opportunities across queries before
// spending any shared budget on a second long reference for one query.
func planLongReferenceCoverage(records []MediaRecord, targets []int, refs map[int][]int, established map[int]bool, remaining float64) [][2]int {
	pairs := [][2]int{}
	first := map[int]int{}
	choose := func(q int, primary bool) {
		for _, r := range refs[q] {
			duration := records[r].Duration
			previous, seen := first[q]
			if (primary && !established[r]) || (seen && previous == r) ||
				duration <= 3600 || duration > 9000 || duration > remaining {
				continue
			}
			pairs = append(pairs, [2]int{r, q})
			remaining -= duration
			if !seen {
				first[q] = r
			}
			break
		}
	}
	for _, q := range targets {
		choose(q, true)
	}
	// Medium clips can also search an unplaced long recorder. Prioritize
	// those that still have no long-reference attempt.
	for _, q := range targets {
		if _, seen := first[q]; !seen && records[q].Duration > 60 {
			choose(q, false)
		}
	}
	for _, q := range targets {
		if records[q].Duration > 60 {
			if r, seen := first[q]; seen && established[r] {
				choose(q, false)
			}
		}
	}
	return pairs
}
