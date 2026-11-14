package autosync

import (
	"context"
	"opensync/internal/storageio"
	"os/exec"
)

type hiddenMediaWindowsKey struct{}

// WithHiddenMediaWindows suppresses auxiliary console windows for desktop
// callers. CLI process behavior and all media command arguments are unchanged.
func WithHiddenMediaWindows(ctx context.Context) context.Context {
	return context.WithValue(ctx, hiddenMediaWindowsKey{}, true)
}
func mediaCommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	scheduled, err := storageio.MediaArgs(ctx, name, args)
	cmd := exec.CommandContext(ctx, name, scheduled...)
	if err != nil {
		cmd.Err = err
	}
	if hidden, _ := ctx.Value(hiddenMediaWindowsKey{}).(bool); hidden {
		hideMediaWindow(cmd)
	}
	return cmd
}
