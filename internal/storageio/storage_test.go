package storageio

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fakeRegistry(infos map[string]Info) *Registry {
	r := NewRegistry()
	r.resolve = func(path string) volumeIdentity { return volumeIdentity{key: path, root: path} }
	r.classify = func(v volumeIdentity) Info { return infos[v.key] }
	return r
}

func waitUntil(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !fn() {
		if time.Now().After(deadline) {
			t.Fatal("condition was not reached")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestPartitionsShareDiskAndOtherDisksRemainParallel(t *testing.T) {
	r := fakeRegistry(map[string]Info{
		"partition-a": {Kind: KindHDD, DiskIDs: []uint32{3}, GroupIDs: []string{"disk:3"}},
		"partition-b": {Kind: KindHDD, DiskIDs: []uint32{3}, GroupIDs: []string{"disk:3"}},
		"other-disk":  {Kind: KindHDD, DiskIDs: []uint32{9}, GroupIDs: []string{"disk:9"}},
		"ssd":         {Kind: KindSSD, DiskIDs: []uint32{1}},
		"ram":         {Kind: KindRAM},
	})
	first, err := r.Acquire(context.Background(), "partition-a")
	if err != nil {
		t.Fatal(err)
	}
	defer first()
	acquired := make(chan func(), 1)
	go func() { release, _ := r.Acquire(context.Background(), "partition-b"); acquired <- release }()
	waitUntil(t, func() bool { stats := r.Snapshot(); return len(stats) == 1 && stats[0].Waiting == 1 })
	for _, path := range []string{"other-disk", "ssd", "ram"} {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		release, err := r.Acquire(ctx, path)
		cancel()
		if err != nil {
			t.Fatalf("independent source %s was blocked: %v", path, err)
		}
		release()
	}
	select {
	case <-acquired:
		t.Fatal("two partitions on the same HDD acquired concurrently")
	default:
	}
	first()
	release := <-acquired
	release()
	release() // Public release is deliberately idempotent.
	if len(r.Snapshot()) != 0 {
		t.Fatal("idle limiter entries retained", r.Snapshot())
	}
}

func TestCancelledSpannedLeaseRollsBackAcquiredDisks(t *testing.T) {
	r := fakeRegistry(map[string]Info{
		"held":    {Kind: KindHDD, GroupIDs: []string{"disk:9"}},
		"spanned": {Kind: KindHDD, GroupIDs: []string{"disk:9", "disk:2", "disk:2"}},
		"disk2":   {Kind: KindHDD, GroupIDs: []string{"disk:2"}},
	})
	held, err := r.Acquire(context.Background(), "held")
	if err != nil {
		t.Fatal(err)
	}
	defer held()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		release, e := r.Acquire(ctx, "spanned")
		if release != nil {
			release()
		}
		result <- e
	}()
	waitUntil(t, func() bool {
		stats := r.Snapshot()
		return len(stats) == 2 && stats[0].ID == "disk:2" && stats[0].Active == 1 && stats[1].Waiting == 1
	})
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation not returned", err)
	}
	release, err := r.Acquire(context.Background(), "disk2")
	if err != nil {
		t.Fatal(err)
	}
	release()
	if stats := r.Snapshot(); len(stats) != 1 || stats[0].ID != "disk:9" || stats[0].Waiting != 0 || stats[0].Active != 1 {
		t.Fatal("cancelled request retained a lease", stats)
	}
	held()
	if len(r.Snapshot()) != 0 {
		t.Fatal("leaked groups")
	}
}

func TestOnePhysicalDiskSerializesConcurrentReads(t *testing.T) {
	r := fakeRegistry(map[string]Info{"hdd": {Kind: KindHDD, GroupIDs: []string{"disk:1"}}})
	var active, peak atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := r.Acquire(context.Background(), "hdd")
			if err != nil {
				t.Error(err)
				return
			}
			count := active.Add(1)
			for p := peak.Load(); count > p; p = peak.Load() {
				if peak.CompareAndSwap(p, count) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			active.Add(-1)
			release()
		}()
	}
	wg.Wait()
	if peak.Load() != 1 || len(r.Snapshot()) != 0 {
		t.Fatal("one-disk exclusivity failed", peak.Load(), r.Snapshot())
	}
}

func TestClassificationIsBoundedSharedAndDefensive(t *testing.T) {
	r := fakeRegistry(nil)
	var calls atomic.Int32
	r.classify = func(v volumeIdentity) Info {
		calls.Add(1)
		time.Sleep(time.Millisecond)
		return Info{Kind: KindHDD, DiskIDs: []uint32{2}, GroupIDs: []string{"disk:2"}}
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r.Inspect("same-volume") }()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("redundant volume classification", calls.Load())
	}
	info := r.Inspect("same-volume")
	info.DiskIDs[0] = 99
	info.GroupIDs[0] = "broken"
	if got := r.Inspect("same-volume"); got.DiskIDs[0] != 2 || got.GroupIDs[0] != "disk:2" {
		t.Fatal("caller changed cache", got)
	}
	for i := 0; i < 150; i++ {
		r.Inspect(fmt.Sprint("volume-", i))
	}
	if len(r.CachedVolumes()) != 128 {
		t.Fatal("classification cache not bounded", len(r.CachedVolumes()))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := calls.Load()
	if _, err := r.Acquire(ctx, "cancelled"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if calls.Load() != before {
		t.Fatal("cancelled request still queried storage")
	}
}

func TestUnknownVolumesUseIndependentConservativeGroups(t *testing.T) {
	r := fakeRegistry(nil)
	r.classify = func(v volumeIdentity) Info { return unknownVolume(v, "test unavailable descriptor") }
	a, _ := r.Acquire(context.Background(), "volume-a")
	defer a()
	b, _ := r.Acquire(context.Background(), "volume-b")
	defer b()
	if !r.Inspect("volume-a").Serialized || len(r.Snapshot()) != 2 {
		t.Fatal(r.Snapshot())
	}
}

func TestInspectNativeVolume(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("native Windows descriptor test")
	}
	path, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{path}
	if extra := os.Getenv("OPENSYNC_STORAGE_TEST_PATHS"); extra != "" {
		paths = append(paths, filepath.SplitList(extra)...)
	}
	for _, p := range paths {
		info := Inspect(p)
		if info.Kind == "" || info.Reason == "" {
			t.Fatal("incomplete storage classification", info)
		}
		t.Logf("%s: %+v", p, info)
	}
}
