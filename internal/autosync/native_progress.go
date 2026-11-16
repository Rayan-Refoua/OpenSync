package autosync

import (
	"context"
	"fmt"
	"os"
	"runtime/pprof"
	"sync"
	"time"
)

type nativeProgress struct {
	profile *os.File
	mu      sync.Mutex
	stage   string
	since   time.Time
	done    chan struct{}
	stopped chan struct{}
	observe func() string
	ctx     context.Context
}

func startNativeProgress(ctx context.Context) *nativeProgress {
	p := &nativeProgress{done: make(chan struct{}), stopped: make(chan struct{}), ctx: ctx}
	go func() {
		defer close(p.stopped)
		ticker := time.NewTicker(time.Second)
		lastPrint := time.Now()
		defer ticker.Stop()
		for {
			select {
			case <-p.done:
				return
			case <-ticker.C:
				p.mu.Lock()
				if p.stage != "" {
					detail := ""
					if p.observe != nil {
						detail = p.observe()
					}
					observe(p.ctx, ProgressEvent{Kind: "heartbeat", Stage: p.stage, Detail: detail, Elapsed: time.Since(p.since).Seconds()})
					if time.Since(lastPrint) >= 10*time.Second && time.Since(p.since) >= 5*time.Second {
						fmt.Printf("WORKING %s — %s elapsed%s\n", p.stage, time.Since(p.since).Round(time.Second), detail)
						lastPrint = time.Now()
					}
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
	p.stopProfile()
	if target := os.Getenv("OPENSYNC_PROFILE_PATH"); target != "" && stage == os.Getenv("OPENSYNC_PROFILE_STAGE") {
		f, err := os.Create(target)
		if err == nil {
			err = pprof.StartCPUProfile(f)
			if err == nil {
				p.profile = f
			} else {
				_ = f.Close()
			}
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "stage profiling: %v\n", err)
		}
	}
	p.stage = stage
	p.since = time.Now()
	fmt.Printf("STAGE %s\n", stage)
	observe(p.ctx, ProgressEvent{Kind: "stage", Stage: stage})
}
func (p *nativeProgress) close()                   { close(p.done); <-p.stopped; p.stopProfile() }
func (p *nativeProgress) monitor(fn func() string) { p.mu.Lock(); p.observe = fn; p.mu.Unlock() }

func (p *nativeProgress) stopProfile() {
	if p.profile != nil {
		pprof.StopCPUProfile()
		_ = p.profile.Close()
		p.profile = nil
	}
}
