package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"opensync/internal/featureindex"
)

type entry struct {
	Source string `json:"source"`
	Cache  string `json:"cache"`
}

type job struct {
	ID    string
	Entry entry
}

func main() {
	manifest := flag.String("manifest",
		"experiments/m2_006/index.json", "audio cache manifest")
	output := flag.String("out",
		"experiments/m3_006/features", "feature index directory")
	workers := flag.Int("workers", 2, "concurrent index workers")
	flag.Parse()

	if *workers < 1 || *workers > 32 {
		fatal(fmt.Errorf("workers must be between 1 and 32"))
	}

	data, err := os.ReadFile(*manifest)
	if err != nil {
		fatal(err)
	}
	var entries map[string]entry
	if err := json.Unmarshal(data, &entries); err != nil {
		fatal(err)
	}
	if len(entries) == 0 {
		fatal(fmt.Errorf("empty manifest"))
	}
	if err := os.MkdirAll(*output, 0755); err != nil {
		fatal(err)
	}

	ids := make([]string, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	jobs := make(chan job)
	var wg sync.WaitGroup
	var completed atomic.Int64
	var skipped atomic.Int64
	var failed atomic.Int64
	var totalBytes atomic.Int64

	start := time.Now()

	fmt.Printf("OpenSync indexer | Go %s | workers=%d | files=%d\n",
		runtime.Version(), *workers, len(ids))

	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for task := range jobs {
				target := filepath.Join(
					*output, task.ID+".osfi")

				// Index keys come from our cache manifest.
				if _, err := os.Stat(target); err == nil {
					idx, err := featureindex.Open(target)
					if err != nil {
						fmt.Printf("INVALID INDEX %s: %v\n",
							task.ID, err)
						failed.Add(1)
						continue
					}
					valid, validationErr := idx.ValidFor(
						task.Entry.Cache)
					idx.Close()
					if validationErr != nil || !valid {
						fmt.Printf("STALE INDEX %s\n", task.ID)
						failed.Add(1)
						continue
					}
					skipped.Add(1)
					continue
				} else if !os.IsNotExist(err) {
					fmt.Printf("INDEX ACCESS ERROR %s: %v\n",
						task.ID, err)
					failed.Add(1)
					continue
				}

				t0 := time.Now()
				err := featureindex.Build(task.Entry.Cache, target)
				if err != nil {
					fmt.Printf("FAILED %s: %v\n", task.ID, err)
					failed.Add(1)
					continue
				}

				// Verify the published index before accepting it.
				idx, err := featureindex.Open(target)
				if err != nil {
					fmt.Printf("VERIFY FAILED %s: %v\n",
						task.ID, err)
					failed.Add(1)
					continue
				}
				valid, err := idx.ValidFor(task.Entry.Cache)
				count := idx.Header.Count
				idx.Close()
				if err != nil || !valid {
					fmt.Printf("SOURCE CHANGED %s\n", task.ID)
					failed.Add(1)
					continue
				}

				st, err := os.Stat(target)
				if err != nil {
					failed.Add(1)
					continue
				}

				totalBytes.Add(st.Size())
				n := completed.Add(1)
				fmt.Printf("[%d/%d] %s frames=%d elapsed=%s\n",
					n, len(ids), task.ID, count,
					time.Since(t0).Round(time.Millisecond))
			}
		}()
	}

	for _, id := range ids {
		jobs <- job{ID: id, Entry: entries[id]}
	}
	close(jobs)
	wg.Wait()

	fmt.Println("=== COLLECTION INDEX RESULT ===")
	fmt.Println("Input files:", len(ids))
	fmt.Println("New indexes:", completed.Load())
	fmt.Println("Reused indexes:", skipped.Load())
	fmt.Println("Failures:", failed.Load())
	fmt.Printf("New index size: %.2f MiB\n",
		float64(totalBytes.Load())/(1024*1024))
	fmt.Println("Elapsed:", time.Since(start).Round(time.Millisecond))
	fmt.Println("Original media: unchanged")

	if failed.Load() != 0 ||
		completed.Load()+skipped.Load() != int64(len(ids)) {
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "ERROR:", err)
	os.Exit(1)
}
