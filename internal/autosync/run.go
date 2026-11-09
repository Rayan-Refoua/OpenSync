// Package autosync runs the reproducible batch DSP pipeline. NumPy/SciPy and
// FFmpeg are explicit runtime dependencies until the DSP is ported to Go.
package autosync

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
)

//go:embed pipeline.py
var pipeline []byte

func Run(ctx context.Context, args []string) error {
	python, err := exec.LookPath("python")
	if err != nil {
		return fmt.Errorf("sync requires Python 3 with NumPy and SciPy: %w", err)
	}
	f, err := os.CreateTemp("", "opensync-pipeline-*.py")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(pipeline); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, python, append([]string{"-u", name}, args...)...)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	return cmd.Run()
}
