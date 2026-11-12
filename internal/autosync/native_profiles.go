package autosync

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

type NativePriorPair struct {
	Reference  int           `json:"reference"`
	Query      int           `json:"query"`
	Candidates []NativePrior `json:"candidates"`
}

func nativeReservoir(ctx context.Context, records []MediaRecord, out string) ([]NativePriorPair, error) {
	pairs := map[[2]int][]NativePrior{}
	for _, namespace := range []string{"evidence-v2", "evidence-recovery-v2", "evidence-refined-v1"} {
		files, err := workspaceGlob(ctx, filepath.Join(out, namespace, "*.json"))
		if err != nil {
			return nil, err
		}
		sort.Strings(files)
		for _, path := range files {
			data, err := workspaceRead(ctx, path)
			if err != nil {
				return nil, err
			}
			var e PairEvidence
			if err = json.Unmarshal(data, &e); err != nil {
				return nil, err
			}
			if e.Reference < 0 || e.Reference >= len(records) || e.Query < 0 || e.Query >= len(records) {
				return nil, fmt.Errorf("invalid cached relation")
			}
			key := [2]int{e.Reference, e.Query}
			for _, c := range e.Candidates {
				var strengths []float64
				rawLo, rawHi, rawCount := math.Inf(1), math.Inf(-1), 0
				for _, o := range c.Observations {
					if o.Supported {
						strengths = append(strengths, o.Wave-o.Control)
					}
					if o.Wave >= .3 && o.Flux >= .5 {
						rawCount++
						rawLo = math.Min(rawLo, o.Offset)
						rawHi = math.Max(rawHi, o.Offset)
					}
				}
				required := 2
				if records[e.Query].Duration >= 30 {
					required = 3
				}
				strength := median(strengths)
				if len(strengths) < required || strength < .05 {
					if !(rawCount >= required && rawHi-rawLo < .1) && !(records[e.Query].Duration < 12 && c.Votes >= 6) {
						continue
					}
					strength = 0
				}
				prior := NativePrior{Offset: c.Offset, Slope: c.Slope, Strength: strength}
				existing := pairs[key]
				near := -1
				for i, p := range existing {
					if math.Abs(p.Offset-prior.Offset) < .5 {
						near = i
						break
					}
				}
				if near < 0 {
					existing = append(existing, prior)
				} else if existing[near].Strength < strength {
					existing[near] = prior
				}
				pairs[key] = existing
			}
		}
	}
	if data, err := workspaceRead(ctx, filepath.Join(out, "distributed-discovery.json")); err == nil {
		var discovery struct {
			Pairs []struct {
				Reference, Query int
				Candidates       []struct{ Offset, Score float64 }
			}
		}
		if err = json.Unmarshal(data, &discovery); err != nil {
			return nil, err
		}
		for _, p := range discovery.Pairs {
			key := [2]int{p.Reference, p.Query}
			existing := pairs[key]
			for _, c := range p.Candidates[:min(3, len(p.Candidates))] {
				distinct := true
				for _, prior := range existing {
					if math.Abs(c.Offset-prior.Offset) < .5 {
						distinct = false
						break
					}
				}
				if distinct {
					existing = append(existing, NativePrior{Offset: c.Offset, Method: "distributed_spectral", Score: c.Score})
				}
			}
			pairs[key] = existing
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	keys := [][2]int{}
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	result := []NativePriorPair{}
	for _, k := range keys {
		ps := pairs[k]
		sort.SliceStable(ps, func(i, j int) bool {
			if ps[i].Strength != ps[j].Strength {
				return ps[i].Strength > ps[j].Strength
			}
			return ps[i].Offset < ps[j].Offset
		})
		result = append(result, NativePriorPair{k[0], k[1], ps})
	}
	return result, nil
}
func matchingLegacyProfiles(data []byte, pair NativePriorPair, ref, query MediaRecord, version string) (*NativeProfilePair, bool) {
	var saved NativeProfilePair
	if json.Unmarshal(data, &saved) != nil || saved.Reference != pair.Reference || saved.Query != pair.Query || len(saved.Profiles) != len(pair.Candidates) {
		return nil, false
	}
	raw, err := rawObject(data)
	if err != nil {
		return nil, false
	}
	profiles, ok := raw["profiles"].([]any)
	if !ok {
		return nil, false
	}
	priors := []any{}
	for i, p := range saved.Profiles {
		want := pair.Candidates[i]
		if p.Prior != want {
			return nil, false
		}
		m, ok := profiles[i].(map[string]any)
		if !ok {
			return nil, false
		}
		priors = append(priors, m["prior"])
	}
	identity, err := pythonJSON(map[string]any{"version": version, "pair": map[string]any{"reference": pair.Reference, "query": pair.Query, "candidates": priors}, "sources": []any{ref.CacheSHA, query.CacheSHA}})
	if err != nil || shaBytes(identity) != saved.Digest {
		return nil, false
	}
	return &saved, true
}
func (a *NativeAnalyzer) runProfiles(ctx context.Context, out string) ([]NativeProfilePair, error) {
	pairs, err := nativeReservoir(a.ctx, a.records, out)
	if err != nil {
		return nil, err
	}
	folder := filepath.Join(out, "multilevel-profile-native-v2")
	if err = workspaceMkdir(a.ctx, folder, 0755); err != nil {
		return nil, err
	}
	results := make([]NativeProfilePair, len(pairs))
	errs := make([]error, len(pairs))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < nativeWorkers(len(pairs)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					continue
				}
				pair := pairs[i]
				ref, query := a.records[pair.Reference], a.records[pair.Query]
				raw, _ := json.Marshal(map[string]any{"version": "multilevel-profile-native-v2", "pair": pair, "sources": []string{ref.CacheSHA, query.CacheSHA}})
				identity := shaBytes(raw)
				name := ref.ID + "_" + query.ID
				path := filepath.Join(folder, name+"_"+identity[:16]+".json")
				if data, err := workspaceRead(a.ctx, path); err == nil {
					var saved NativeProfilePair
					if json.Unmarshal(data, &saved) == nil && saved.Digest == identity {
						results[i] = saved
						continue
					}
				}
				reused := false
				for _, version := range []string{"multilevel-profile-v7", "multilevel-profile-v3"} {
					if query.Duration >= 2 && query.Duration <= 12 {
						continue
					}
					if version == "multilevel-profile-v3" && (ref.Duration < 70 || query.Duration < 30) {
						continue
					}
					files, _ := workspaceGlob(a.ctx, filepath.Join(out, version, name+"*.json"))
					sort.Strings(files)
					for _, old := range files {
						data, err := workspaceRead(a.ctx, old)
						if err != nil {
							errs[i] = err
							break
						}
						if saved, ok := matchingLegacyProfiles(data, pair, ref, query, version); ok {
							for _, p := range saved.Profiles {
								evaluateNativeProfile(p)
							}
							saved.Digest = identity
							results[i] = *saved
							errs[i] = workspaceJSON(a.ctx, path, saved)
							reused = true
							break
						}
					}
					if reused || errs[i] != nil {
						break
					}
				}
				if reused || errs[i] != nil {
					continue
				}
				result := NativeProfilePair{Reference: pair.Reference, Query: pair.Query, Digest: identity, Profiles: []*NativeProfile{}}
				for _, prior := range pair.Candidates {
					if ctx.Err() != nil {
						break
					}
					p, err := a.profileCandidate(pair.Reference, pair.Query, prior)
					if err != nil {
						errs[i] = err
						break
					}
					result.Profiles = append(result.Profiles, p)
				}
				if errs[i] == nil && ctx.Err() == nil {
					validated := false
					for _, p := range result.Profiles {
						if p.Status == "validated_relation" {
							validated = true
							break
						}
					}
					if !validated {
						p, err := a.shortPartitionProfile(pair.Reference, pair.Query)
						if err != nil {
							errs[i] = err
						} else if p != nil {
							result.Profiles = append(result.Profiles, p)
						}
					}
				}
				if errs[i] == nil && ctx.Err() == nil {
					errs[i] = workspaceJSON(a.ctx, path, result)
					results[i] = result
				}
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
	if err = workspaceJSON(a.ctx, filepath.Join(out, "multilevel-profiles.json"), map[string]any{"version": "multilevel-profile-native-v2", "implementation": "native-go-v2", "pairs": results, "baseline_positions_used": false}); err != nil {
		return nil, err
	}
	fmt.Printf("MULTILEVEL: %d pairs evaluated\n", len(results))
	return results, nil
}
