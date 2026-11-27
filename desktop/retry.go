package main

import (
	"context"
	"fmt"
	"time"

	"opensync/internal/autosync"
	"opensync/internal/desktop"
)

type retryResult struct {
	Project *desktop.Project `json:"project,omitempty"`
	Added   int              `json:"added"`
}

func (a *App) RetryClips(options autosync.RetryOptions) (retryResult, error) {
	return a.runMatchRevision("Retry selected clips", func(ctx context.Context, path string) (string, int, error) {
		return autosync.RetryRecordings(ctx, path, options)
	})
}

func (a *App) VerifyReviewMatches() (retryResult, error) {
	return a.runMatchRevision("Verify matches with waveform", autosync.VerifyReviewWaveforms)
}

func (a *App) runMatchRevision(stage string, work func(context.Context, string) (string, int, error)) (result retryResult, err error) {
	a.mu.Lock()
	p := a.media.Project()
	if a.running || p == nil || p.Path == "" {
		a.mu.Unlock()
		return result, fmt.Errorf("open a saved project and wait for the current operation")
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.running = true
	a.cancel = cancel
	a.outcome, a.stage, a.detail = "running", stage, ""
	a.stageStarted = time.Now()
	a.stageCompleted, a.stageTotal = 0, 0
	a.clipActivity = map[int]string{}
	a.mu.Unlock()
	defer func() {
		cancel()
		a.mu.Lock()
		a.cancel = nil
		a.running = false
		a.stage = "Review recordings"
		a.outcome = "ready"
		if err != nil {
			a.detail = err.Error()
		}
		a.mu.Unlock()
	}()
	ctx = autosync.WithHiddenMediaWindows(autosync.WithProgressObserver(ctx, a.progress))
	a.media.PausePreview()
	defer a.media.ResumePreview()
	path, added, err := work(ctx, p.Path)
	if err != nil || added == 0 {
		return result, err
	}
	next, err := desktop.LoadProject(path, a.GetSettings().QueueUncertain)
	if err != nil {
		return result, err
	}
	if err := saveActiveReview(reviewBase(p), path); err != nil {
		return result, err
	}
	a.media.SetProject(next)
	return retryResult{Project: next, Added: added}, nil
}
