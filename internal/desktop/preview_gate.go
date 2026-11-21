package desktop

import (
	"context"
	"errors"
	"sync"
)

var errPreviewPaused = errors.New("preview paused during synchronization")

type previewGate struct {
	mu     sync.Mutex
	paused bool
	active sync.WaitGroup
	life   context.Context
	cancel context.CancelFunc
}

func (g *previewGate) enter(ctx context.Context) (context.Context, func(), error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.paused {
		return nil, nil, errPreviewPaused
	}
	if g.life == nil {
		g.life, g.cancel = context.WithCancel(context.Background())
	}
	child, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(g.life, cancel)
	g.active.Add(1)
	return child, func() { stop(); cancel(); g.active.Done() }, nil
}

// PausePreview cancels current reads, then drains them before sync starts.
func (s *Service) PausePreview() {
	g := &s.preview
	g.mu.Lock()
	g.paused = true
	if g.cancel != nil {
		g.cancel()
	}
	g.mu.Unlock()
	g.active.Wait()
}

func (s *Service) ResumePreview() {
	g := &s.preview
	g.mu.Lock()
	defer g.mu.Unlock()
	g.life, g.cancel = nil, nil
	g.paused = false
}
