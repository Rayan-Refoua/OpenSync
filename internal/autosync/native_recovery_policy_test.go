package autosync

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDistributedBudgetLeavesExplicitUnsearchedWork(t *testing.T) {
	records := []MediaRecord{{Duration: 120}, {Duration: 30}, {Duration: 8}}
	remaining := 60.
	selected, deferred := capDistributedPairs(records, [][2]int{{0, 2}, {1, 2}}, &remaining, nil, "spectral")
	if !reflect.DeepEqual(selected, [][2]int{{1, 2}}) || remaining != 30 || len(deferred) != 1 || deferred[0]["coverage"] != "unsearched" || deferred[0]["reference"] != 0 {
		t.Fatalf("selected %v, remaining %v, deferred %v", selected, remaining, deferred)
	}
	selected, deferred = capDistributedPairs(records, [][2]int{{1, 2}}, &remaining, deferred, "neighboring_onset")
	if len(selected) != 1 || remaining != 0 || len(deferred) != 1 {
		t.Fatalf("second pass did not share the cap: %v %v %v", selected, remaining, deferred)
	}
}

func TestEnvelopeCheckpointRetainsCompletedEmptySearch(t *testing.T) {
	out := t.TempDir()
	if err := os.Mkdir(filepath.Join(out, "recovery-pairs-v1"), 0755); err != nil {
		t.Fatal(err)
	}
	a := NewNativeAnalyzer([]MediaRecord{{ID: "ref", CacheSHA: "r", Duration: 30}, {ID: "query", CacheSHA: "q", Duration: 12}})
	defer a.Close()
	pair := NativePair{Reference: 0, Query: 1}
	first, err := a.cachedEnvelopePair(context.Background(), out, pair, make([][3][]float32, 2))
	if err != nil || len(first) != 0 {
		t.Fatalf("empty search: %v, %v", first, err)
	}
	// No feature buffers are available on a resume hit.
	again, err := a.cachedEnvelopePair(context.Background(), out, pair, nil)
	if err != nil || len(again) != 0 {
		t.Fatalf("empty checkpoint: %v, %v", again, err)
	}
}
