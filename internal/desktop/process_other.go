//go:build !windows

package desktop

import "os/exec"

func hideProcess(cmd *exec.Cmd) {}
