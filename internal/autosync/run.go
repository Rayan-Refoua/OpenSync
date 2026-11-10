// Package autosync runs the reproducible batch DSP pipeline. NumPy/SciPy and
// FFmpeg are explicit runtime dependencies until the DSP is ported to Go.
package autosync

import (
	"context"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

//go:embed pipeline.py project_export.py refinement.py relation_graph.py time_mapping.py source_metadata.py distributed_recovery.py acoustic_context.py
var runtimeFiles embed.FS

func Run(ctx context.Context, args []string) error {
	python, runtimeBin := "", ""
	if executable, err := os.Executable(); err == nil {
		runtimeDir := filepath.Join(filepath.Dir(executable), "runtime")
		localPython := filepath.Join(runtimeDir, "python", "python.exe")
		if info, statErr := os.Stat(localPython); statErr == nil && !info.IsDir() {
			python = localPython
			runtimeBin = filepath.Join(runtimeDir, "bin")
		}
	}
	if python == "" {
		var err error
		python, err = exec.LookPath("python")
		if err != nil {
			return fmt.Errorf("OpenSync runtime is missing; use the complete packaged distribution: %w", err)
		}
	}
	dir, err := os.MkdirTemp("", "opensync-runtime-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for _, name := range []string{"pipeline.py", "project_export.py", "refinement.py", "relation_graph.py", "time_mapping.py", "source_metadata.py", "distributed_recovery.py", "acoustic_context.py"} {
		data, readErr := runtimeFiles.ReadFile(name)
		if readErr != nil {
			return readErr
		}
		if err = os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			return err
		}
	}
	cmd := exec.CommandContext(ctx, python, append([]string{"-X", "utf8", "-u", filepath.Join(dir, "pipeline.py")}, args...)...)
	cmd.Env = os.Environ()
	if executable, err := os.Executable(); err == nil {
		cmd.Env = append(cmd.Env, "OPENSYNC_NATIVE_EXECUTABLE="+executable)
	}
	if runtimeBin != "" {
		cmd.Env = append(cmd.Env, "PATH="+runtimeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	return cmd.Run()
}
