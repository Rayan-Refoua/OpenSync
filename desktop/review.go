package main

import (
	"encoding/json"
	"fmt"
	wr "github.com/wailsapp/wails/v2/pkg/runtime"
	"opensync/internal/autosync"
	"opensync/internal/desktop"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (a *App) ReviewSolution(change autosync.ReviewChange) (result *desktop.Project, err error) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return nil, fmt.Errorf("wait for the current operation")
	}
	p := a.media.Project()
	if p == nil || p.Path == "" {
		a.mu.Unlock()
		return nil, fmt.Errorf("open a saved project first")
	}
	a.running = true
	a.stage = "Re-evaluating saved evidence"
	a.stageStarted = time.Now()
	a.outcome = "running"
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.running = false
		if err != nil {
			a.stage = "Review failed"
			a.outcome = "failed"
			a.detail = err.Error()
		}
		a.mu.Unlock()
	}()
	path, err := autosync.CreateSolution(p.Path, change)
	if err != nil {
		a.log("error", err.Error())
		return nil, err
	}
	next, err := desktop.LoadProject(path, a.GetSettings().QueueUncertain)
	if err != nil {
		return nil, err
	}
	if err = saveActiveReview(reviewBase(p), path); err != nil {
		return nil, err
	}
	a.media.SetProject(next)
	a.mu.Lock()
	a.stage = "Review revision saved"
	a.outcome = "ready"
	detail := "Original project retained; no acoustic extraction repeated"
	a.detail = detail
	a.mu.Unlock()
	a.log("review", detail+" · "+path)
	return next, nil
}

func (a *App) UndoReview() (*desktop.Project, error) {
	p := a.media.Project()
	if p == nil {
		return nil, fmt.Errorf("no project")
	}
	v, _ := p.Graph["solution_revision"].(map[string]any)
	parent, _ := v["parent_project"].(string)
	if parent == "" {
		return nil, fmt.Errorf("already at the original project")
	}
	return a.SelectHistoryRevision(parent)
}

type SolutionHistoryItem struct {
	Path    string `json:"path"`
	Label   string `json:"label"`
	Created string `json:"created"`
}

func (a *App) SolutionHistory() ([]SolutionHistoryItem, error) {
	p := a.media.Project()
	if p == nil || p.Path == "" {
		return nil, nil
	}
	base := p.Path
	if v, ok := p.Graph["solution_revision"].(map[string]any); ok {
		if b, ok := v["base_project"].(string); ok {
			base = b
		}
	}
	result := []SolutionHistoryItem{{Path: base, Label: "Original synchronization"}}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(base), "Solutions", "solution-*.opensync"))
	if err != nil {
		return nil, err
	}
	if len(files) > 2000 {
		return nil, fmt.Errorf("too many solution revisions in this folder")
	}
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var wrapper autosync.NativeProject
		if json.Unmarshal(b, &wrapper) != nil || wrapper.Format != "opensync.solution" {
			continue
		}
		var delta autosync.SolutionDelta
		if json.Unmarshal(wrapper.Payload, &delta) != nil {
			continue
		}
		resolved := delta.Base
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(filepath.Dir(path), resolved)
		}
		if filepath.Clean(resolved) != filepath.Clean(base) {
			continue
		}
		result = append(result, SolutionHistoryItem{Path: path, Label: delta.Label, Created: delta.Created})
	}
	sort.Slice(result[1:], func(i, j int) bool { return result[i+1].Created < result[j+1].Created })
	return result, nil
}

func (a *App) ExportProjectXML() (output string, err error) {
	a.mu.Lock()
	busy := a.running
	a.mu.Unlock()
	if busy {
		return "", fmt.Errorf("wait for the current operation")
	}
	p := a.media.Project()
	if p == nil || p.Path == "" {
		return "", fmt.Errorf("open a saved project first")
	}
	root, err := wr.OpenDirectoryDialog(a.ctx, wr.OpenDialogOptions{Title: "Choose parent folder for a new Premiere export"})
	if err != nil || root == "" {
		return "", err
	}
	out := filepath.Join(root, "OpenSync-Export-"+time.Now().Format("20060102-150405.000"))
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return "", fmt.Errorf("wait for the current operation")
	}
	a.running = true
	a.outcome = "running"
	a.stage = "Exporting Premiere XML"
	a.stageStarted = time.Now()
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.running = false
		a.stage = "Project ready"
		a.outcome = "ready"
		if err != nil {
			a.stage = "Export failed"
			a.outcome = "failed"
			a.detail = err.Error()
		}
		a.mu.Unlock()
	}()
	settings := a.GetSettings()
	if settings.UnmatchedPolicy == "" {
		settings.UnmatchedPolicy = "append"
	}
	if settings.MaxResidualMS == 0 {
		settings.MaxResidualMS = 150
	}
	if err = autosync.ExportNative(p.Path, autosync.NativeExportOptions{AudioEarlyMS: settings.AudioEarlyMS, AudioLateMS: settings.AudioLateMS, Output: out, Unsynced: settings.UnmatchedPolicy, TrackLayout: settings.TrackLayout, KeepTrackOrder: settings.KeepTrackOrder, OmitProjectMarker: settings.OmitProjectMarker, MaxResidualMS: settings.MaxResidualMS, IncludeProvisional: settings.IncludeProvisional, UseOriginalAudio: settings.UseOriginalAudio}); err != nil {
		a.log("error", err.Error())
		return "", err
	}
	path := filepath.Join(out, "OpenSync.xml")
	a.log("export", "Premiere XML saved · "+path)
	return path, nil
}

func reviewBase(p *desktop.Project) string {
	revision, _ := p.Graph["solution_revision"].(map[string]any)
	if base, ok := revision["base_project"].(string); ok {
		return base
	}
	return p.Path
}

// Each immutable solution project retains its parent. This small cursor lets
// opening the original project resume the selected branch after a restart.
func activeReviewPath(base string) (string, error) {
	b, err := os.ReadFile(base + ".active-review")
	if os.IsNotExist(err) {
		return base, nil
	}
	if err != nil {
		return "", err
	}
	name := string(b)
	if name != filepath.Base(name) || !strings.HasPrefix(name, "solution-") || filepath.Ext(name) != ".opensync" {
		return "", fmt.Errorf("invalid saved review selection")
	}
	return filepath.Join(filepath.Dir(base), "Solutions", name), nil
}

func saveActiveReview(base, path string) error {
	dest := base + ".active-review"
	if filepath.Clean(path) == filepath.Clean(base) {
		err := os.Remove(dest)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(base), ".review-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	_, err = temp.WriteString(filepath.Base(path))
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temp.Name(), dest)
}
