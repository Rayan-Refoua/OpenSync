package autosync

import (
	"context"
	"fmt"
	"opensync/internal/storageio"
	"sync"
	"time"
)

// ioProbe compares useful PCM throughput on one physical device. Admission
// changes only at file boundaries, so a slowdown never discards decoded audio.
type ioProbe struct {
	mu            sync.Mutex
	changed       chan struct{}
	active, limit int
	phase         int
	started       time.Time
	bytes         int64
	baseline      float64
}

func newIOProbe() *ioProbe {
	return &ioProbe{changed: make(chan struct{}), limit: 1}
}

func (p *ioProbe) wake() {
	close(p.changed)
	p.changed = make(chan struct{})
}

func (p *ioProbe) acquire(ctx context.Context) (func(), error) {
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
		p.bytes = 0
		if p.phase == 1 {
			p.phase = 2
		}
	}
	p.mu.Unlock()
	return func() {
		p.mu.Lock()
		p.active--
		if p.phase == 2 {
			// A decoder ended before a comparable concurrent window was collected.
			p.phase = 1
		}
		if p.phase == 0 {
			// Short files should not delay other work to obtain a baseline.
			p.phase = 3
			p.limit = 2
		}
		p.wake()
		p.mu.Unlock()
	}, nil
}

func (p *ioProbe) observe(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.phase != 0 && p.phase != 2 {
		return
	}
	p.bytes += int64(n)
	elapsed := time.Since(p.started)
	if p.phase == 0 && elapsed >= 2*time.Second {
		p.baseline = float64(p.bytes) / elapsed.Seconds()
		p.phase = 1
		p.limit = 2
		p.wake()
	}
	if p.phase == 2 && elapsed >= 20*time.Second {
		rate := float64(p.bytes) / elapsed.Seconds()
		if rate < p.baseline*.95 {
			p.limit = 1
		}
		p.phase = 3
		fmt.Printf("I/O probe: one reader %.2f MiB/s, two readers %.2f MiB/s; limit %d\n", p.baseline/(1<<20), rate/(1<<20), p.limit)
	}
}

func ioProbeKey(path string) string {
	info := storageio.Inspect(path)
	if len(info.DiskIDs) == 1 {
		return fmt.Sprintf("disk:%d", info.DiskIDs[0])
	}
	if len(info.DiskIDs) > 1 {
		return ""
	}
	return info.Volume
}
