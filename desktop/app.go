package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	wr "github.com/wailsapp/wails/v2/pkg/runtime"
	"opensync/internal/autosync"
	"opensync/internal/desktop"
	"opensync/internal/storageio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Event struct {
	Seq       int64  `json:"seq"`
	Time      string `json:"time"`
	Kind      string `json:"kind"`
	Message   string `json:"message"`
	Recording *int   `json:"recording,omitempty"`
}
type App struct {
	ctx                         context.Context
	media                       *desktop.Service
	mu                          sync.Mutex
	events                      []Event
	seq                         int64
	stage, detail               string
	running                     bool
	ioProgress                  *autosync.IOProgress
	cancel                      context.CancelFunc
	inputs                      []string
	diagnosticWritten           bool
	clipActivity                map[int]string
	settings                    WorkstationSettings
	resources                   *desktop.ResourceMonitor
	stageStarted                time.Time
	outcome                     string
	stageCompleted, stageTotal  int
	previewActive, previewReady int
}

// Opt-in smoke instrumentation does not run or write files in ordinary use.
func (a *App) ReportPreviewState(active, ready int) {
	if os.Getenv("OPENSYNC_DESKTOP_SMOKE") == "" || active < 0 || active > 16 || ready < 0 || ready > active {
		return
	}
	a.mu.Lock()
	a.previewActive, a.previewReady = active, ready
	a.mu.Unlock()
}

func newApp() *App {
	a := &App{media: desktop.NewService(), stage: "Ready", clipActivity: map[int]string{}, resources: desktop.NewResourceMonitor(), stageStarted: time.Now()}
	a.loadSettings()
	return a
}
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	for _, arg := range os.Args[1:] {
		if strings.EqualFold(filepath.Ext(arg), ".opensync") {
			if _, e := a.OpenProject(arg); e != nil {
				a.log("error", e.Error())
			}
			break
		}
	}
}
func (a *App) log(kind, message string) {
	a.logRecording(kind, message, nil)
}
func (a *App) logRecording(kind, message string, recording *int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.seq++
	a.events = append(a.events, Event{Seq: a.seq, Time: time.Now().Format("15:04:05"), Kind: kind, Message: message, Recording: recording})
	if len(a.events) > 256 {
		a.events = a.events[len(a.events)-256:]
	}
}
func (a *App) GetProject() *desktop.Project { return a.media.Project() }
func (a *App) OpenProject(path string) (*desktop.Project, error) {
	a.mu.Lock()
	busy := a.running
	a.mu.Unlock()
	if busy {
		return nil, fmt.Errorf("stop the active sync before opening another project")
	}
	p, e := desktop.LoadProject(path)
	if e != nil {
		return nil, e
	}
	a.media.SetProject(p)
	a.mu.Lock()
	a.outcome = "ready"
	a.stage = "Project ready"
	a.detail = ""
	a.mu.Unlock()
	a.log("project", fmt.Sprintf("Opened %s · %d recordings · project integrity verified", p.Name, len(p.Clips)))
	return p, nil
}
func (a *App) ChooseProject() (*desktop.Project, error) {
	path, e := wr.OpenFileDialog(a.ctx, wr.OpenDialogOptions{Title: "Open OpenSync project", Filters: []wr.FileFilter{{DisplayName: "OpenSync project", Pattern: "*.opensync"}}})
	if e != nil || path == "" {
		return nil, e
	}
	return a.OpenProject(path)
}

func (a *App) OpenDroppedProject(paths []string) (*desktop.Project, error) {
	path, err := droppedProjectPath(paths)
	if err != nil {
		return nil, err
	}
	return a.OpenProject(path)
}

func droppedProjectPath(paths []string) (string, error) {
	if len(paths) != 1 || paths[0] == "" {
		return "", errors.New("drop one .opensync file or one OpenSync project folder")
	}
	path := paths[0]
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("cannot access dropped item: %w", err)
	}
	if info.IsDir() {
		project := filepath.Join(path, "OpenSync.opensync")
		info, err = os.Stat(project)
		if errors.Is(err, os.ErrNotExist) || (err == nil && !info.Mode().IsRegular()) {
			return "", fmt.Errorf("%s does not contain an OpenSync.opensync project file", filepath.Base(path))
		}
		if err != nil {
			return "", fmt.Errorf("cannot access OpenSync.opensync: %w", err)
		}
		return project, nil
	}
	if !info.Mode().IsRegular() || !strings.EqualFold(filepath.Ext(path), ".opensync") {
		return "", errors.New("drop a .opensync file or a folder containing OpenSync.opensync")
	}
	return path, nil
}
func (a *App) LocateMedia() (*desktop.Project, error) {
	root, e := wr.OpenDirectoryDialog(a.ctx, wr.OpenDialogOptions{Title: "Locate original media folder (includes subfolders)"})
	if e != nil || root == "" {
		return nil, e
	}
	p, e := a.media.Relink(root)
	if e == nil {
		a.log("media", "Located media in "+root+" · session only; saved project unchanged")
	}
	return p, e
}
func (a *App) SelectInputs(folder bool) (int, error) {
	a.mu.Lock()
	busy := a.running
	a.mu.Unlock()
	if busy {
		return 0, fmt.Errorf("sync is already running")
	}
	var paths []string
	var e error
	if folder {
		paths, e = selectMediaFolders(a.ctx)
	} else {
		paths, e = wr.OpenMultipleFilesDialog(a.ctx, wr.OpenDialogOptions{Title: "Add recordings"})
	}
	if e != nil {
		return 0, e
	}
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return 0, fmt.Errorf("sync is already running")
	}
	for _, p := range paths {
		found := false
		for _, q := range a.inputs {
			found = found || strings.EqualFold(p, q)
		}
		if !found {
			a.inputs = append(a.inputs, p)
		}
	}
	n := len(a.inputs)
	a.mu.Unlock()
	a.log("input", fmt.Sprintf("%d files/folders selected for next sync", n))
	return n, nil
}
func (a *App) ClearInputs() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.running {
		a.inputs = nil
	}
}
func (a *App) Status(after int64) map[string]any {
	a.mu.Lock()
	events := []Event{}
	for _, e := range a.events {
		if e.Seq > after {
			events = append(events, e)
		}
	}
	r := map[string]any{"ioProgress": a.ioProgress, "running": a.running, "stage": a.stage, "detail": a.detail, "events": events, "inputs": len(a.inputs), "version": autosync.BuildVersion}
	activity := map[int]string{}
	for k, v := range a.clipActivity {
		activity[k] = v
	}
	r["clipActivity"] = activity
	r["stageSeconds"] = time.Since(a.stageStarted).Seconds()
	r["outcome"] = a.outcome
	r["stageCompleted"], r["stageTotal"] = a.stageCompleted, a.stageTotal
	a.mu.Unlock()
	if p := a.media.Project(); p != nil {
		r["generation"] = p.Generation
	}
	r["visualization"] = a.media.Stats()
	r["resources"] = a.resources.Sample()
	r["storage"] = storageio.Snapshot()
	r["storageVolumes"] = storageio.CachedVolumes()
	// Opt-in hidden launch smoke test; no diagnostic file is written normally.
	if path := os.Getenv("OPENSYNC_DESKTOP_SMOKE"); path != "" {
		r["diagnostic"] = true
		a.mu.Lock()
		r["previewActive"], r["previewReady"] = a.previewActive, a.previewReady
		if !a.diagnosticWritten && a.previewActive > 0 && a.previewReady == a.previewActive && a.media.Stats()["completed"].(int64) > 0 {
			if data, err := json.MarshalIndent(r, "", "  "); err == nil {
				if os.WriteFile(path, data, 0600) == nil {
					a.diagnosticWritten = true
				}
			}
		}
		a.mu.Unlock()
	}
	return r
}
func (a *App) CancelSync() {
	a.mu.Lock()
	cancel := a.cancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
func (a *App) StartSync() error {
	return a.startSync(nil, "")
}
func (a *App) ResynchronizeProject() error {
	p := a.media.Project()
	if p == nil || p.Path == "" {
		return fmt.Errorf("open a saved project first")
	}
	var paths []string
	for _, c := range p.Clips {
		paths = append(paths, c.Source)
	}
	return a.startSync(paths, p.Path)
}
func (a *App) startSync(override []string, corrections string) error {
	a.mu.Lock()
	busy := a.running
	paths := append([]string(nil), a.inputs...)
	if override != nil {
		paths = override
	}
	a.mu.Unlock()
	if busy {
		return fmt.Errorf("sync is already running")
	}
	if len(paths) == 0 {
		return fmt.Errorf("add recordings or folders first")
	}
	root, e := wr.OpenDirectoryDialog(a.ctx, wr.OpenDialogOptions{Title: "Choose parent folder for a new OpenSync project"})
	if e != nil || root == "" {
		return e
	}
	out := filepath.Join(root, "OpenSync-"+time.Now().Format("20060102-150405"))
	ctx, cancel := context.WithCancel(a.ctx)
	ctx = autosync.WithHiddenMediaWindows(ctx)
	ctx = autosync.WithProgressObserver(ctx, a.progress)
	var peakWriteWarning sync.Once
	ctx = autosync.WithFingerprintPCM(ctx, func(record autosync.MediaRecord) func([]byte) {
		return a.media.SyncWaveformTo(record, out, func(err error) {
			peakWriteWarning.Do(func() { a.log("warning", "Could not save waveform peaks: "+err.Error()) })
		})
	})
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		cancel()
		return fmt.Errorf("sync is already running")
	}
	a.running = true
	a.outcome = "running"
	a.stageStarted = time.Now()
	a.stageCompleted, a.stageTotal = 0, 0
	a.clipActivity = map[int]string{}
	a.cancel = cancel
	a.stage = "Starting synchronization"
	a.ioProgress = nil
	a.mu.Unlock()
	a.log("sync", "Native Go synchronization started · disk cache disabled · "+out)
	settings := a.GetSettings()
	go func() {
		prior := runtime.GOMAXPROCS(settings.Resources.Workers)
		defer cancel()
		o := autosync.NativeSyncOptions{Output: out, Name: "OpenSync", Stage: "all", Unsynced: "append", MaxResidualMS: 150}
		o.Mode = settings.Mode
		o.Analysis = settings.Analysis
		o.TrackLayout = settings.TrackLayout
		o.IncludeProvisional = settings.IncludeProvisional
		if settings.MaxResidualMS > 0 {
			o.MaxResidualMS = settings.MaxResidualMS
		}
		o.Unsynced = settings.UnmatchedPolicy
		if o.Unsynced == "" {
			o.Unsynced = "append"
		}
		o.AudioEarlyMS, o.AudioLateMS = settings.AudioEarlyMS, settings.AudioLateMS
		o.Corrections = corrections
		o.ManagedMemoryMiB = settings.Resources.MemoryMiB
		for _, p := range paths {
			if st, e := os.Stat(p); e == nil && st.IsDir() {
				o.Folders = append(o.Folders, p)
			} else {
				o.Files = append(o.Files, p)
			}
		}
		a.media.PausePreview()
		err := autosync.NativeSync(ctx, o)
		a.media.ResumePreview()
		runtime.GOMAXPROCS(prior)
		a.mu.Lock()
		a.running = false
		a.cancel = nil
		a.detail = ""
		if err != nil {
			a.stage = "Sync stopped"
			a.outcome = "failed"
			if errors.Is(err, context.Canceled) {
				a.outcome = "cancelled"
			}
			a.detail = err.Error()
		} else {
			a.stage = "Sync complete"
			a.outcome = "complete"
		}
		a.mu.Unlock()
		if err != nil {
			a.log("error", err.Error())
			return
		}
		if _, err = a.OpenProject(filepath.Join(out, "OpenSync.opensync")); err != nil {
			a.mu.Lock()
			a.outcome = "failed"
			a.detail = "Saved project could not be reopened: " + err.Error()
			a.mu.Unlock()
			a.log("error", err.Error())
		} else {
			a.mu.Lock()
			a.outcome = "complete"
			a.stage = "Sync complete"
			a.mu.Unlock()
			a.log("complete", "Project and Premiere XML saved in "+out)
		}
	}()
	return nil
}
func (a *App) progress(e autosync.ProgressEvent) {
	if e.Kind == "io_read" {
		a.mu.Lock()
		a.ioProgress = e.IO
		a.mu.Unlock()
		return
	}
	if e.Kind == "pair" {
		a.mu.Lock()
		a.stageCompleted, a.stageTotal = e.Completed, e.Total
		for _, i := range []int{e.Reference, e.Query} {
			a.clipActivity[i] = e.Status
		}
		a.mu.Unlock()
		i := e.Query
		a.logRecording("pair", fmt.Sprintf("%s · %d/%d · recordings %d ↔ %d · %s", e.Stage, e.Completed, e.Total, e.Reference+1, e.Query+1, e.Status), &i)
	}
	if e.Kind == "stage" || e.Kind == "heartbeat" {
		a.mu.Lock()
		a.stage = e.Stage
		a.detail = e.Detail
		a.mu.Unlock()
	}
	if e.Kind == "stage" {
		a.mu.Lock()
		a.stageStarted = time.Now()
		a.ioProgress = nil
		a.stageCompleted, a.stageTotal = 0, 0
		a.mu.Unlock()
		a.log("stage", e.Stage)
	}
	if e.Kind == "evidence" {
		a.log("evidence", fmt.Sprintf("%s: %d / %d pairs; final validation pending", e.Stage, e.Completed, e.Total))
	}
	if e.Kind == "inventory" || e.Kind == "layout" {
		payload := autosync.NativePayload{Name: "Live synchronization", Records: e.Records, Graph: map[string]any{}}
		for _, v := range e.Layout {
			origin := 0.
			if r := e.Records[v.Recording]; r.Video != nil && r.Audio != nil {
				origin = asFloat(r.Audio["start_time"]) - asFloat(r.Video["start_time"])
			}
			knots := append([]autosync.TimePoint(nil), v.Knots...)
			for i := range knots {
				knots[i][0] += origin
			}
			status := "provisional_live_" + v.Status
			if strings.HasPrefix(v.Status, "unresolved") {
				status = v.Status
			}
			payload.Models = append(payload.Models, autosync.TimeModel{Recording: v.Recording, Component: v.Component, ComponentSize: v.ComponentSize, Offset: v.Start - v.Rate*origin, AudioOrigin: origin, Rate: v.Rate, Error: v.Error, Status: status, Knots: knots, Resolved: !strings.HasPrefix(v.Status, "unresolved")})
		}
		a.media.SetProject(desktop.BuildProject(payload))
		a.log(e.Kind, fmt.Sprintf("%d recordings · %s", len(e.Records), e.Detail))
	}
}
func asFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case json.Number:
		f, _ := n.Float64()
		return f
	case string:
		var f float64
		fmt.Sscan(n, &f)
		return f
	}
	return 0
}
