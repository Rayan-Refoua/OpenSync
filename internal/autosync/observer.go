package autosync

import "context"

// ProgressEvent is observational only. It never participates in evidence,
// scheduling or acceptance decisions. Callbacks must return promptly.
type ProgressEvent struct {
	Kind      string         `json:"kind"`
	Stage     string         `json:"stage,omitempty"`
	Detail    string         `json:"detail,omitempty"`
	Elapsed   float64        `json:"elapsed,omitempty"`
	Records   []MediaRecord  `json:"records,omitempty"`
	Layout    []NativeLayout `json:"layout,omitempty"`
	Completed int            `json:"completed,omitempty"`
	Total     int            `json:"total,omitempty"`
}
type observerKey struct{}

func WithProgressObserver(ctx context.Context, fn func(ProgressEvent)) context.Context {
	return context.WithValue(ctx, observerKey{}, fn)
}
func observe(ctx context.Context, e ProgressEvent) {
	if fn, ok := ctx.Value(observerKey{}).(func(ProgressEvent)); ok {
		fn(e)
	}
}
