package desktop

import (
	"math"
	"runtime"
	"testing"
	"time"
)

func TestResourceSettingsRejectUnenforceableValues(t *testing.T) {
	s := DefaultResourceSettings()
	if err := ValidateResourceSettings(s); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*ResourceSettings){
		func(s *ResourceSettings) { s.MemoryMiB = 0 },
		func(s *ResourceSettings) { s.Workers = runtime.NumCPU() + 1 },
		func(s *ResourceSettings) { s.Background = "unbounded" },
		func(s *ResourceSettings) { s.DecodeWorkers = 0 },
		func(s *ResourceSettings) { s.ThumbnailWorkers = 100 },
		func(s *ResourceSettings) { s.MaxVideoFeeds = 17 },
	} {
		bad := s
		change(&bad)
		if ValidateResourceSettings(bad) == nil {
			t.Fatalf("accepted unsupported resource settings: %+v", bad)
		}
	}
}

func TestResourceCountersSeparateAppTreeAndSystem(t *testing.T) {
	start := time.Now()
	before := resourceReading{At: start, Root: 1, Processes: map[uint32]processUsage{1: {PID: 1, Created: 10, CPU100NS: 100}, 2: {PID: 2, Created: 20, CPU100NS: 100}, 3: {PID: 3, Created: 30, CPU100NS: 500}}, System: systemUsage{Kernel: 100, User: 100, Idle: 50}}
	after := resourceReading{At: start.Add(time.Second), Root: 1, Processes: map[uint32]processUsage{1: {PID: 1, Created: 10, CPU100NS: 100 + 10_000_000, WorkingSet: 100, Private: 90}, 2: {PID: 2, Name: "msedgewebview2.exe", Created: 20, CPU100NS: 100 + 20_000_000, WorkingSet: 300, Private: 200}, 3: {PID: 3, Name: "ffmpeg.exe", Created: 999, CPU100NS: 90_000_000, WorkingSet: 50, Private: 40}}, System: systemUsage{Kernel: 200, User: 200, Idle: 100, TotalRAM: 1000, FreeRAM: 250}}
	s := summarizeResourceUsage(after, before, 4)
	if !s.CPUReady || math.Abs(s.AppCPUPercent-25) > 1e-9 || math.Abs(s.TreeCPUPercent-75) > 1e-9 {
		t.Fatalf("CPU normalization or PID reuse accounting incorrect: %+v", s)
	}
	if s.SystemCPUPercent != 75 || s.AppWorkingSetBytes != 100 || s.TreeWorkingSetBytes != 450 || s.WebviewWorkingSetBytes != 300 || s.DecoderWorkingSetBytes != 50 || s.SystemMemoryUsedBytes != 750 {
		t.Fatalf("resource categories mixed: %+v", s)
	}
	if summarizeResourceUsage(after, resourceReading{}, 4).CPUReady {
		t.Fatal("first sample claimed a measured CPU percentage")
	}
}

func TestProcessTreeIndependentOfEnumerationOrder(t *testing.T) {
	ids := descendantIDs(1, []processUsage{{PID: 3, Parent: 2}, {PID: 90, Parent: 91}, {PID: 2, Parent: 1}, {PID: 1, Parent: 0}, {PID: 4, Parent: 3}})
	if len(ids) != 4 || !ids[4] || ids[90] {
		t.Fatal(ids)
	}
}

func TestResourceMonitorSampling(t *testing.T) {
	m := NewResourceMonitor()
	s := m.Sample()
	if runtime.GOOS != "windows" {
		if s.Available || s.Error == "" {
			t.Fatalf("unsupported platform reported measurements: %+v", s)
		}
		return
	}
	if !s.Available || s.AppWorkingSetBytes == 0 || s.SystemMemoryTotalBytes == 0 || s.ProcessCount < 1 {
		t.Fatalf("Windows process counters unavailable: %+v", s)
	}
	if m.Sample().TimestampMS != s.TimestampMS || len(m.History()) != 1 {
		t.Fatal("monitor did not cache the one-second sample")
	}
	history := m.History()
	history[0].ProcessCount = -1
	if m.History()[0].ProcessCount < 1 {
		t.Fatal("history caller can mutate monitor state")
	}
}
