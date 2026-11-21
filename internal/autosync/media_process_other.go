//go:build !windows

package autosync

import "os/exec"

func hideMediaWindow(cmd *exec.Cmd) {}
