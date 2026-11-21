package desktop

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ResourceSettings describes controls the desktop can actually apply. MemoryMiB
// is a managed-memory budget, not an OS-enforced process-tree working-set cap:
// Go's memory target is soft and WebView2/driver allocations belong to Windows.
type ResourceSettings struct {
	MemoryMiB        int    `json:"memoryMiB"`
	Workers          int    `json:"workers"`
	Background       string `json:"background"`
	DecodeWorkers    int    `json:"decodeWorkers"`
	ThumbnailWorkers int    `json:"thumbnailWorkers"`
	MaxVideoFeeds    int    `json:"maxVideoFeeds"`
}

func DefaultResourceSettings() ResourceSettings {
	return ResourceSettings{MemoryMiB: 4096, Workers: max(1, runtime.NumCPU()-2), Background: "balanced", DecodeWorkers: 2, ThumbnailWorkers: 1, MaxVideoFeeds: 4}
}

func ValidateResourceSettings(s ResourceSettings) error {
	if s.MemoryMiB < 256 || s.MemoryMiB > 131072 {
		return fmt.Errorf("managed memory must be between 256 and 131072 MiB")
	}
	if s.Workers < 1 || s.Workers > runtime.NumCPU() {
		return fmt.Errorf("CPU workers must be between 1 and %d", runtime.NumCPU())
	}
	if s.Background != "off" && s.Background != "low" && s.Background != "balanced" {
		return fmt.Errorf("background processing must be off, low or balanced")
	}
	if s.DecodeWorkers < 1 || s.DecodeWorkers > 8 {
		return fmt.Errorf("media decoder concurrency must be between 1 and 8")
	}
	if s.ThumbnailWorkers < 1 || s.ThumbnailWorkers > 4 {
		return fmt.Errorf("thumbnail concurrency must be between 1 and 4")
	}
	if s.MaxVideoFeeds < 1 || s.MaxVideoFeeds > 16 {
		return fmt.Errorf("video feed limit must be between 1 and 16")
	}
	return nil
}

// ResourceSample distinguishes OpenSync's own process, its currently running
// descendants, and the whole machine. CPU percentages use all logical CPUs as
// 100%; working-set sums can include shared pages more than once. PrivateBytes
// is private committed memory, not private resident memory.
type ResourceSample struct {
	Available              bool    `json:"available"`
	Error                  string  `json:"error,omitempty"`
	TimestampMS            int64   `json:"timestampMS"`
	LogicalCPUs            int     `json:"logicalCPUs"`
	AppCPUPercent          float64 `json:"appCPUPercent"`
	AppWorkingSetBytes     uint64  `json:"appWorkingSetBytes"`
	AppPrivateBytes        uint64  `json:"appPrivateBytes"`
	TreeCPUPercent         float64 `json:"treeCPUPercent"`
	TreeWorkingSetBytes    uint64  `json:"treeWorkingSetBytes"`
	TreePrivateBytes       uint64  `json:"treePrivateBytes"`
	WebviewWorkingSetBytes uint64  `json:"webviewWorkingSetBytes"`
	DecoderWorkingSetBytes uint64  `json:"decoderWorkingSetBytes"`
	ProcessCount           int     `json:"processCount"`
	SystemCPUPercent       float64 `json:"systemCPUPercent"`
	SystemMemoryUsedBytes  uint64  `json:"systemMemoryUsedBytes"`
	SystemMemoryTotalBytes uint64  `json:"systemMemoryTotalBytes"`
	CPUReady               bool    `json:"cpuReady"`
}

type processUsage struct {
	PID, Parent uint32
	Name        string
	Created     uint64
	CPU100NS    uint64
	WorkingSet  uint64
	Private     uint64
}

type systemUsage struct {
	Idle, Kernel, User uint64
	TotalRAM, FreeRAM  uint64
}

type resourceReading struct {
	At        time.Time
	Root      uint32
	Processes map[uint32]processUsage
	System    systemUsage
}

// ResourceMonitor samples on demand at most once per second. It does not start
// a polling goroutine, query process command lines, or launch PowerShell/WMI.
type ResourceMonitor struct {
	mu       sync.Mutex
	previous resourceReading
	last     ResourceSample
	history  []ResourceSample
}

func NewResourceMonitor() *ResourceMonitor { return &ResourceMonitor{} }

func (m *ResourceMonitor) Sample() ResourceSample {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if m.last.TimestampMS != 0 && now.UnixMilli()-m.last.TimestampMS < 1000 {
		return m.last
	}
	reading, err := readResourceUsage()
	if err != nil {
		m.last = ResourceSample{TimestampMS: now.UnixMilli(), LogicalCPUs: runtime.NumCPU(), Error: err.Error()}
		return m.last
	}
	reading.At = now
	m.last = summarizeResourceUsage(reading, m.previous, runtime.NumCPU())
	m.previous = reading
	m.history = append(m.history, m.last)
	if len(m.history) > 60 {
		copy(m.history, m.history[len(m.history)-60:])
		m.history = m.history[:60]
	}
	return m.last
}

func (m *ResourceMonitor) History() []ResourceSample {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ResourceSample(nil), m.history...)
}

func summarizeResourceUsage(current, previous resourceReading, cpus int) ResourceSample {
	r := ResourceSample{Available: true, TimestampMS: current.At.UnixMilli(), LogicalCPUs: max(1, cpus), ProcessCount: len(current.Processes), SystemMemoryTotalBytes: current.System.TotalRAM}
	if current.System.TotalRAM >= current.System.FreeRAM {
		r.SystemMemoryUsedBytes = current.System.TotalRAM - current.System.FreeRAM
	}
	elapsed := current.At.Sub(previous.At).Seconds()
	r.CPUReady = !previous.At.IsZero() && elapsed > 0
	for pid, p := range current.Processes {
		var cpu float64
		if before, ok := previous.Processes[pid]; r.CPUReady && ok && before.Created == p.Created && p.CPU100NS >= before.CPU100NS {
			cpu = 100 * float64(p.CPU100NS-before.CPU100NS) / (elapsed * 1e7 * float64(r.LogicalCPUs))
		}
		r.TreeCPUPercent += cpu
		r.TreeWorkingSetBytes += p.WorkingSet
		r.TreePrivateBytes += p.Private
		if pid == current.Root {
			r.AppCPUPercent = cpu
			r.AppWorkingSetBytes = p.WorkingSet
			r.AppPrivateBytes = p.Private
		}
		name := strings.ToLower(p.Name)
		if strings.Contains(name, "msedgewebview2") {
			r.WebviewWorkingSetBytes += p.WorkingSet
		} else if name == "ffmpeg.exe" || name == "ffprobe.exe" || name == "ffmpeg" || name == "ffprobe" {
			r.DecoderWorkingSetBytes += p.WorkingSet
		}
	}
	r.AppCPUPercent = min(100, r.AppCPUPercent)
	r.TreeCPUPercent = min(100, r.TreeCPUPercent)
	s, before := current.System, previous.System
	if r.CPUReady && s.Kernel >= before.Kernel && s.User >= before.User && s.Idle >= before.Idle {
		total := (s.Kernel - before.Kernel) + (s.User - before.User)
		idle := s.Idle - before.Idle
		if total > 0 && idle <= total {
			r.SystemCPUPercent = 100 * float64(total-idle) / float64(total)
		}
	}
	return r
}

// descendantIDs resolves all descendant levels without assuming Toolhelp's
// enumeration order. A PID is only a membership hint; creation times protect
// CPU deltas against PID reuse between samples.
func descendantIDs(root uint32, processes []processUsage) map[uint32]bool {
	children := make(map[uint32][]uint32)
	for _, p := range processes {
		children[p.Parent] = append(children[p.Parent], p.PID)
	}
	seen := map[uint32]bool{root: true}
	queue := []uint32{root}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		for _, pid := range children[parent] {
			if !seen[pid] {
				seen[pid] = true
				queue = append(queue, pid)
			}
		}
	}
	return seen
}
