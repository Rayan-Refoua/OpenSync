package main

import (
	"encoding/json"
	"fmt"
	"opensync/internal/autosync"
	"opensync/internal/desktop"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
)

type WorkstationSettings struct {
	FileTimePlacement  *bool                     `json:"fileTimePlacement,omitempty"`
	Analysis           autosync.AnalysisSettings `json:"analysis"`
	TrackLayout        string                    `json:"trackLayout"`
	MaxResidualMS      float64                   `json:"maxResidualMS"`
	IncludeProvisional bool                      `json:"includeProvisional"`
	UnmatchedPolicy    string                    `json:"unmatchedPolicy"`
	AudioEarlyMS       float64                   `json:"audioEarlyMS"`
	AudioLateMS        float64                   `json:"audioLateMS"`
	Mode               string                    `json:"mode"`
	Resources          desktop.ResourceSettings  `json:"resources"`
	Shortcuts          map[string]string         `json:"shortcuts"`
	Layout             MonitorLayout             `json:"layout"`
}
type MonitorLayout struct {
	Position string `json:"position"`
	Rows     int    `json:"rows"`
	Columns  int    `json:"columns"`
}

func normalizeLayout(s *WorkstationSettings) error {
	if err := s.Analysis.Normalize(); err != nil {
		return err
	}
	if s.TrackLayout == "" {
		s.TrackLayout = "family"
	}
	if s.TrackLayout != "family" && s.TrackLayout != "folder" {
		return fmt.Errorf("invalid Premiere track layout")
	}
	if s.MaxResidualMS == 0 {
		s.MaxResidualMS = 150
	}
	if !(s.MaxResidualMS >= 1 && s.MaxResidualMS <= 150) {
		return fmt.Errorf("export presentation residual must be 1..150 ms")
	}

	if s.AudioEarlyMS < 0 || s.AudioLateMS < 0 {
		return fmt.Errorf("audio drift tolerances must be positive")
	}
	if s.AudioEarlyMS == 0 {
		s.AudioEarlyMS = 50
	}
	if s.AudioLateMS == 0 {
		s.AudioLateMS = 70
	}
	if s.UnmatchedPolicy == "" {
		s.UnmatchedPolicy = "append"
	}
	switch s.UnmatchedPolicy {
	case "append", "separate-sequence", "order_only", "remove", "disabled", "local-tracks":
	default:
		return fmt.Errorf("invalid unmatched export policy")
	}
	if s.Mode == "" {
		s.Mode = "clips"
	}
	if s.Mode != "clips" && s.Mode != "chronological" {
		return fmt.Errorf("invalid synchronization mode")
	}
	if s.Layout.Position == "" {
		s.Layout = MonitorLayout{Position: "right", Rows: 4}
	}
	if (s.Layout.Position != "right" && s.Layout.Position != "top") || s.Layout.Rows < 1 || s.Layout.Rows > 16 || s.Layout.Columns < 0 || s.Layout.Columns > 4 {
		return fmt.Errorf("invalid monitor layout")
	}
	return nil
}

func defaultSettings() WorkstationSettings {
	return WorkstationSettings{MaxResidualMS: 150, TrackLayout: "family", AudioEarlyMS: 50, AudioLateMS: 70, Mode: "clips", Resources: desktop.DefaultResourceSettings(), Shortcuts: map[string]string{}, Layout: MonitorLayout{Position: "right", Rows: 4}}
}
func settingsPath() string {
	root, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(root, "OpenSync", "workstation-settings.json")
}
func (a *App) loadSettings() {
	s := defaultSettings()
	if b, e := os.ReadFile(settingsPath()); e == nil {
		var saved WorkstationSettings
		if json.Unmarshal(b, &saved) == nil && desktop.ValidateResourceSettings(saved.Resources) == nil && normalizeLayout(&saved) == nil {
			s = saved
		}
	}
	a.settings = s
	a.applyResources(s.Resources)
}
func (a *App) GetSettings() WorkstationSettings {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.settings
	s.Shortcuts = map[string]string{}
	for k, v := range a.settings.Shortcuts {
		s.Shortcuts[k] = v
	}
	return s
}
func (a *App) UpdateSettings(s WorkstationSettings) error {
	if err := normalizeLayout(&s); err != nil {
		return err
	}
	if err := desktop.ValidateResourceSettings(s.Resources); err != nil {
		return err
	}
	if len(s.Shortcuts) > 50 {
		return fmt.Errorf("too many shortcuts")
	}
	for k, v := range s.Shortcuts {
		if len(k) > 50 || len(v) > 80 {
			return fmt.Errorf("invalid shortcut")
		}
	}
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return fmt.Errorf("stop synchronization before changing its resource limits")
	}
	a.mu.Unlock()
	path := settingsPath()
	if path == "" {
		return fmt.Errorf("settings location unavailable")
	}
	b, e := json.MarshalIndent(s, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	if e = os.WriteFile(path+".tmp", b, 0600); e != nil {
		return e
	}
	if e = os.Rename(path+".tmp", path); e != nil {
		return e
	}
	a.mu.Lock()
	a.settings = s
	a.mu.Unlock()
	a.applyResources(s.Resources)
	a.log("settings", "Resource limits and shortcuts updated")
	return nil
}
func (a *App) applyResources(s desktop.ResourceSettings) {
	debug.SetMemoryLimit(int64(s.MemoryMiB) * 3 / 4 << 20)
	wave, thumb := s.DecodeWorkers, s.ThumbnailWorkers
	if s.Background == "low" {
		wave, thumb = 1, 1
	}
	_ = a.media.SetVisualizationLimits(min(512, max(16, s.MemoryMiB/8)), wave, thumb)
	a.media.SetPlaybackWorkers(s.DecodeWorkers)
}
func (a *App) OpenOutputFolder() error {
	p := a.media.Project()
	if p == nil || p.Path == "" {
		return fmt.Errorf("no saved output folder yet")
	}
	folder := filepath.Dir(p.Path)
	if revision, ok := p.Graph["solution_revision"].(map[string]any); ok {
		if base, ok := revision["base_project"].(string); ok && base != "" {
			folder = filepath.Dir(base)
		}
	}
	if _, e := os.Stat(folder); e != nil {
		return e
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer.exe", folder).Start()
	case "darwin":
		return exec.Command("open", folder).Start()
	default:
		return exec.Command("xdg-open", folder).Start()
	}
}
