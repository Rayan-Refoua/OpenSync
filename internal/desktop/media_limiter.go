package desktop

import (
	"context"
	"fmt"
	"sync"
)

// mediaLimiter changes the admission limit without replacing channels under
// running jobs. Lower limits take effect as existing work finishes.
type mediaLimiter struct {
	mu            sync.Mutex
	active, limit int
	wake          chan struct{}
}

func newMediaLimiter(limit int) *mediaLimiter {
	return &mediaLimiter{limit: limit, wake: make(chan struct{})}
}
func (l *mediaLimiter) acquire(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		l.mu.Lock()
		if l.active < l.limit {
			l.active++
			l.mu.Unlock()
			return nil
		}
		wake := l.wake
		l.mu.Unlock()
		select {
		case <-wake:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
func (l *mediaLimiter) release() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.active--
	close(l.wake)
	l.wake = make(chan struct{})
}
func (l *mediaLimiter) set(limit int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.limit = limit
	close(l.wake)
	l.wake = make(chan struct{})
}
func (l *mediaLimiter) stats() (int, int) { l.mu.Lock(); defer l.mu.Unlock(); return l.active, l.limit }

// SetVisualizationLimits controls actual cache budgets and decoder admission.
// It does not claim to cap total WebView/GPU/process memory or engine workers.
func (s *Service) SetVisualizationLimits(memoryMiB, waveformWorkers, thumbnailWorkers int) error {
	if memoryMiB < 16 || memoryMiB > 4096 {
		return fmt.Errorf("visualization RAM must be 16..4096 MiB")
	}
	if waveformWorkers < 1 || waveformWorkers > 32 {
		return fmt.Errorf("waveform workers must be 1..32")
	}
	if thumbnailWorkers < 1 || thumbnailWorkers > 16 {
		return fmt.Errorf("thumbnail workers must be 1..16")
	}
	total := memoryMiB << 20
	s.cache.setLimit(total / 4)
	s.peaks.setLimit(total - total/4)
	s.visual.set(waveformWorkers)
	s.peaks.pending.set(waveformWorkers * 2)
	s.thumbs.set(thumbnailWorkers)
	return nil
}
