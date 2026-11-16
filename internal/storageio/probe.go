package storageio

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// IOProbe compares useful PCM throughput on one physical device. Admission
// changes only at file boundaries, so a slowdown never discards decoded audio.
type IOProbe struct {
	mu            sync.Mutex
	changed       chan struct{}
	active, limit int
	phase         int
	started       time.Time
	bytes         int64
	baseline      float64
	shortJobs     bool
	busy          time.Duration
}

func NewIOProbe() *IOProbe {
	return &IOProbe{changed: make(chan struct{}), limit: 1}
}

// NewPreviewProbe accumulates measurements across short, successful jobs.
func NewPreviewProbe() *IOProbe {
	p := NewIOProbe()
	p.shortJobs = true
	return p
}

func (p *IOProbe) wake() {
	close(p.changed)
	p.changed = make(chan struct{})
}

func (p *IOProbe) Acquire(ctx context.Context) (func(), error) {
	p.mu.Lock()
	for p.active >= p.limit && ctx.Err() == nil {
		changed := p.changed
		p.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
		}
		p.mu.Lock()
	}
	if err := ctx.Err(); err != nil {
		p.mu.Unlock()
		return nil, err
	}
	p.active++
	if p.phase == 0 && p.started.IsZero() || p.phase == 1 && p.active == 2 {
		p.started = time.Now()
		if !p.shortJobs {
			p.bytes = 0
		}
		if p.phase == 1 {
			p.phase = 2
		}
	}
	p.mu.Unlock()
	return func() {
		p.mu.Lock()
		p.active--
		if p.shortJobs && (p.phase == 0 || p.phase == 2) {
			p.busy += time.Since(p.started)
			p.started = time.Time{}
		}
		if p.phase == 2 {
			// A decoder ended before a comparable concurrent window was collected.
			p.phase = 1
		}
		if p.phase == 0 && !p.shortJobs {
			// Short files should not delay other work to obtain a baseline.
			p.phase = 3
			p.limit = 2
		}
		p.wake()
		p.mu.Unlock()
	}, nil
}

func (p *IOProbe) Observe(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.phase != 0 && p.phase != 2 {
		return
	}
	p.bytes += int64(n)
	elapsed := p.busy + time.Since(p.started)
	if p.phase == 0 && elapsed >= 2*time.Second {
		p.baseline = float64(p.bytes) / elapsed.Seconds()
		p.phase = 1
		p.busy = 0
		p.bytes = 0
		p.limit = 2
		p.wake()
	}
	if p.phase == 2 && elapsed >= 20*time.Second {
		rate := float64(p.bytes) / elapsed.Seconds()
		if rate < p.baseline*.95 {
			p.limit = 1
		}
		p.phase = 3
		if !p.shortJobs {
			fmt.Printf("I/O probe: one reader %.2f MiB/s, two readers %.2f MiB/s; limit %d\n", p.baseline/(1<<20), rate/(1<<20), p.limit)
		}
	}
}

func IOProbeKey(path string) string {
	info := Inspect(path)
	if len(info.DiskIDs) == 1 {
		return fmt.Sprintf("disk:%d", info.DiskIDs[0])
	}
	if len(info.DiskIDs) > 1 {
		return ""
	}
	return info.Volume
}
