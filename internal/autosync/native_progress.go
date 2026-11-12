package autosync

import (
	"fmt"
	"sync"
	"time"
)

type nativeProgress struct {
	mu      sync.Mutex
	stage   string
	since   time.Time
	done    chan struct{}
	stopped chan struct{}
	observe func() string
}

func startNativeProgress() *nativeProgress {
	p := &nativeProgress{done: make(chan struct{}), stopped: make(chan struct{})}
	go func() {
		defer close(p.stopped)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-p.done:
				return
			case <-ticker.C:
				p.mu.Lock()
				if p.stage != "" && time.Since(p.since) >= 5*time.Second {
					detail := ""
					if p.observe != nil {
						detail = p.observe()
					}
					fmt.Printf("WORKING %s — %s elapsed%s\n", p.stage, time.Since(p.since).Round(time.Second), detail)
				}
				p.mu.Unlock()
			}
		}
	}()
	return p
}
func (p *nativeProgress) set(stage string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stage = stage
	p.since = time.Now()
	fmt.Printf("STAGE %s\n", stage)
}
func (p *nativeProgress) close()                   { close(p.done); <-p.stopped }
func (p *nativeProgress) monitor(fn func() string) { p.mu.Lock(); p.observe = fn; p.mu.Unlock() }
