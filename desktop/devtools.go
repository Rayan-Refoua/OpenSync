package main

import (
	"fmt"
	"os"
	"os/exec"

	wr "github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) RestartWithGUIDebug() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running {
		return fmt.Errorf("wait for the active operation before restarting")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"--debug-ui"}
	if project := a.media.Project(); project != nil && project.Path != "" {
		args = append(args, project.Path)
	}
	command := exec.Command(exe, args...)
	if err := command.Start(); err != nil {
		return err
	}
	_ = command.Process.Release()
	a.running = true
	go wr.Quit(a.ctx)
	return nil
}
