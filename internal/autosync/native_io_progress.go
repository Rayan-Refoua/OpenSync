package autosync

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"
)

type IOFileProgress struct {
	Name     string  `json:"name"`
	Fraction float64 `json:"fraction"`
	ETA      float64 `json:"eta"`
}

type IOProgress struct {
	Remaining int              `json:"remaining"`
	Total     int              `json:"total"`
	ETA       float64          `json:"eta"`
	Files     []IOFileProgress `json:"files"`
}

type ioReadProgress struct {
	mu               sync.Mutex
	ctx              context.Context
	start, last      time.Time
	expected, worked float64
	probeSeconds     float64
	remaining, total int
	active           map[int]IOFileProgress
}

func newIOReadProgress(ctx context.Context, total int, expected float64) *ioReadProgress {
	return &ioReadProgress{ctx: ctx, start: time.Now(), remaining: total, total: total, expected: expected, active: map[int]IOFileProgress{}}
}

func (p *ioReadProgress) report(index int, name string, started time.Time, done, expected, delta float64, finished bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.worked += delta
	if finished {
		delete(p.active, index)
		p.remaining--
		p.expected += done - expected
		if expected == 1 && done == 1 {
			p.probeSeconds += time.Since(started).Seconds()
		}
	} else {
		elapsed := time.Since(started).Seconds()
		entry := IOFileProgress{Name: name, ETA: -1, Fraction: -1}
		if expected > 0 && done > 0 {
			entry.Fraction = math.Min(1, done/expected)
			if elapsed >= .25 && done < expected {
				entry.ETA = (expected - done) * elapsed / done
			}
		}
		if expected == 0 && p.probeSeconds > 0 && p.total > p.remaining {
			entry.ETA = math.Max(0, p.probeSeconds/float64(p.total-p.remaining)-elapsed)
		}
		p.active[index] = entry
	}
	now := time.Now()
	if !finished && now.Sub(p.last) < 250*time.Millisecond && done != 0 {
		return
	}
	p.last = now
	out := &IOProgress{Remaining: p.remaining, Total: p.total, ETA: -1, Files: []IOFileProgress{}}
	elapsed := now.Sub(p.start).Seconds()
	if p.worked > 0 && elapsed >= .25 && p.expected >= p.worked {
		out.ETA = (p.expected - p.worked) * elapsed / p.worked
	}
	keys := make([]int, 0, len(p.active))
	for i := range p.active {
		keys = append(keys, i)
	}
	sort.Ints(keys)
	for _, i := range keys {
		out.Files = append(out.Files, p.active[i])
	}
	observe(p.ctx, ProgressEvent{Kind: "io_read", IO: out})
}

func finiteDuration(v float64) float64 {
	if v > 0 && !math.IsInf(v, 0) {
		return v
	}
	return 0
}
