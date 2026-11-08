package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"opensync/internal/audio"
	"opensync/internal/syncgraph"
)

type CacheEntry struct {
	Source          string  `json:"source"`
	Cache           string  `json:"cache"`
	DurationSeconds float64 `json:"duration_seconds"`
}

func main() {
	var err error

	switch {
	case len(os.Args) == 3 && os.Args[1] == "inspect":
		err = inspect(os.Args[2])

	case len(os.Args) == 3 && os.Args[1] == "solve":
		err = solveDiagnostic(os.Args[2])

	case len(os.Args) == 4 && os.Args[1] == "review-sweep":
		err = reviewSweepCommand(os.Args[2], os.Args[3])

	case len(os.Args) == 5 && os.Args[1] == "audit-premiere":
		err = auditPremiere(os.Args[2], os.Args[3], os.Args[4])

	case len(os.Args) == 7 && os.Args[1] == "export-full-preview":
		err = exportFullPreview(os.Args[2], os.Args[3], os.Args[4], os.Args[5], os.Args[6])

	case len(os.Args) == 5 && os.Args[1] == "import-discovery":
		err = importDiscovery(os.Args[2], os.Args[3], os.Args[4])

	case len(os.Args) == 4 && os.Args[1] == "propose-tracks":
		err = proposeTracks(os.Args[2], os.Args[3])

	case len(os.Args) == 5 && os.Args[1] == "probe":
		err = probeHypothesis(os.Args[2], os.Args[3], os.Args[4])

	case len(os.Args) == 6 && os.Args[1] == "probe":
		err = probeHypothesisPreset(os.Args[2], os.Args[3], os.Args[4], os.Args[5])

	case len(os.Args) == 7 && os.Args[1] == "sequence-probe":
		err = sequenceBatchCommand(os.Args[2], os.Args[3], os.Args[4], os.Args[5], os.Args[6])

	case len(os.Args) == 4 && os.Args[1] == "sequence-plan":
		err = sequencePlanCommand(os.Args[2], os.Args[3])

	case len(os.Args) == 7 && os.Args[1] == "recover":
		err = recoverRecording(os.Args[2], os.Args[3], os.Args[4], os.Args[5], os.Args[6])

	case len(os.Args) == 3 && os.Args[1] == "preset":
		err = showPreset(os.Args[2])

	case len(os.Args) == 5 && os.Args[1] == "export-premiere":
		err = exportPremiere(os.Args[2], os.Args[3], os.Args[4])

	case len(os.Args) == 6 && os.Args[1] == "export-report":
		err = exportReport(os.Args[2], os.Args[3], os.Args[4], os.Args[5])

	case len(os.Args) == 7 && os.Args[1] == "export-measured":
		err = exportMeasured(
			os.Args[2], os.Args[3], os.Args[4],
			os.Args[5], os.Args[6],
		)

	default:
		fmt.Fprintln(os.Stderr,
			"Usage: opensync inspect <cache-index.json> | solve <constraints.json> | import-discovery <inventory.json> <discovery.json> <project.json>")
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}

func inspect(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var index map[string]CacheEntry
	if err := json.Unmarshal(data, &index); err != nil {
		return err
	}
	if len(index) == 0 {
		return fmt.Errorf("empty cache index")
	}

	keys := make([]string, 0, len(index))
	for k := range index {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var totalFrames uint64
	var totalBytes uint64
	var invalid int

	for _, id := range keys {
		e := index[id]
		info, err := audio.Inspect(e.Cache)
		if err != nil {
			fmt.Printf("INVALID %s: %v\n", id, err)
			invalid++
			continue
		}
		if info.SampleRate != 16000 ||
			info.Channels != 1 || info.Bits != 16 {
			fmt.Printf("INVALID %s: unexpected PCM format\n", id)
			invalid++
			continue
		}
		actualDuration := float64(info.Frames) /
			float64(info.SampleRate)
		delta := actualDuration - e.DurationSeconds
		if delta < -0.25 || delta > 0.25 {
			fmt.Printf("INVALID %s: duration mismatch %.3fs\n",
				id, delta)
			invalid++
			continue
		}
		totalFrames += info.Frames
		totalBytes += info.Bytes
	}

	graph := syncgraph.New()

	fmt.Println("=== OPENSYNC NATIVE CORE ===")
	fmt.Println("Cache entries:", len(index))
	fmt.Println("Valid entries:", len(index)-invalid)
	fmt.Println("Invalid entries:", invalid)
	fmt.Printf("Cached audio: %.3f GiB\n",
		float64(totalBytes)/(1024*1024*1024))
	fmt.Printf("Combined duration: %.2f hours\n",
		float64(totalFrames)/16000/3600)
	fmt.Println("Verified graph edges:", graph.Count())
	fmt.Println("Timeline: unchanged")
	fmt.Println("Source video: untouched")

	if invalid != 0 {
		return fmt.Errorf("%d cache entries failed validation", invalid)
	}
	return nil
}
