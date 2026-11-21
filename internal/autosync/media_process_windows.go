//go:build windows

package autosync

import (
	"os/exec"
	"syscall"
)

func hideMediaWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
