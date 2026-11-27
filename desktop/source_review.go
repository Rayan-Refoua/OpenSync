package main

import (
	"context"
	"fmt"

	"opensync/internal/autosync"
)

type pendingSourceReview struct {
	ID    int                 `json:"id"`
	Plan  autosync.SourcePlan `json:"plan"`
	reply chan autosync.SourcePlan
}

func (a *App) sourceReviewer(mode string) func(context.Context, autosync.SourcePlan) (autosync.SourcePlan, error) {
	return func(ctx context.Context, plan autosync.SourcePlan) (autosync.SourcePlan, error) {
		if mode != "always" && (mode != "ambiguous" || len(plan.Warnings) == 0) {
			return plan, nil
		}
		a.mu.Lock()
		a.sourceReviewID++
		pending := &pendingSourceReview{ID: a.sourceReviewID, Plan: plan, reply: make(chan autosync.SourcePlan, 1)}
		a.sourceReview = pending
		a.stage = "Review recording sources"
		a.mu.Unlock()
		defer func() {
			a.mu.Lock()
			a.sourceReview = nil
			a.mu.Unlock()
		}()
		select {
		case <-ctx.Done():
			return autosync.SourcePlan{}, ctx.Err()
		case reviewed := <-pending.reply:
			return reviewed, ctx.Err()
		}
	}
}

func (a *App) SubmitSourceReview(id int, reviewed autosync.SourcePlan) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	pending := a.sourceReview
	if pending == nil || pending.ID != id {
		return fmt.Errorf("source review is no longer active")
	}
	select {
	case pending.reply <- reviewed:
		a.sourceReview = nil
		return nil
	default:
		return fmt.Errorf("source review was already submitted")
	}
}
