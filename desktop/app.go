package main

import (
	"context"
	"fmt"
	wr "github.com/wailsapp/wails/v2/pkg/runtime"
	"opensync/internal/autosync"
	"opensync/internal/desktop"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Event struct {
	Seq     int64  `json:"seq"`
	Time    string `json:"time"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}
type App struct {
	ctx           context.Context
	media         *desktop.Service
	mu            sync.Mutex
	events        []Event
	seq           int64
	stage, detail string
	running       bool
	cancel        context.CancelFunc
	inputs        []string
}

func newApp() *App { return &App{media: desktop.NewService(), stage: "Ready"} }
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
	a.mu.Lock()
	defer a.mu.Unlock()
	a.seq++
	a.events = append(a.events, Event{a.seq, time.Now().Format("15:04:05"), kind, message})
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
		var p string
		p, e = wr.OpenDirectoryDialog(a.ctx, wr.OpenDialogOptions{Title: "Add media folder (recursive)"})
		if p != "" {
			paths = []string{p}
		}
	} else {
		paths, e = wr.OpenMultipleFilesDialog(a.ctx, wr.OpenDialogOptions{Title: "Add recordings"})
	}
	if e != nil {
		return 0, e
	}
	a.mu.Lock()
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
	r := map[string]any{"running": a.running, "stage": a.stage, "detail": a.detail, "events": events, "inputs": len(a.inputs), "version": autosync.BuildVersion}
	a.mu.Unlock()
	if p := a.media.Project(); p != nil {
		r["generation"] = p.Generation
	}
	r["visualization"] = a.media.Stats()
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
	a.mu.Lock()
	busy := a.running
	paths := append([]string(nil), a.inputs...)
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
	ctx = autosync.WithProgressObserver(ctx, a.progress)
	a.mu.Lock()
	a.running = true
	a.cancel = cancel
	a.stage = "Starting synchronization"
	a.mu.Unlock()
	a.log("sync", "Native Go synchronization started · disk cache disabled · "+out)
	go func() {
		defer cancel()
		o := autosync.NativeSyncOptions{Output: out, Name: "OpenSync", Stage: "all", Unsynced: "append", MaxResidualMS: 150}
		for _, p := range paths {
			if st, e := os.Stat(p); e == nil && st.IsDir() {
				o.Folders = append(o.Folders, p)
			} else {
				o.Files = append(o.Files, p)
			}
		}
		err := autosync.NativeSync(ctx, o)
		a.mu.Lock()
		a.running = false
		a.cancel = nil
		a.detail = ""
		if err != nil {
			a.stage = "Sync stopped"
		} else {
			a.stage = "Sync complete"
		}
		a.mu.Unlock()
		if err != nil {
			a.log("error", err.Error())
			return
		}
		if _, err = a.OpenProject(filepath.Join(out, "OpenSync.opensync")); err != nil {
			a.log("error", err.Error())
		} else {
			a.log("complete", "Project and Premiere XML saved in "+out)
		}
	}()
	return nil
}
func (a *App) progress(e autosync.ProgressEvent) {
	if e.Kind == "stage" || e.Kind == "heartbeat" {
		a.mu.Lock()
		a.stage = e.Stage
		a.detail = e.Detail
		a.mu.Unlock()
	}
	if e.Kind == "stage" {
		a.log("stage", e.Stage)
	}
	if e.Kind == "evidence" {
		a.log("evidence", fmt.Sprintf("%s: %d / %d pairs; final validation pending", e.Stage, e.Completed, e.Total))
	}
	if e.Kind == "inventory" || e.Kind == "layout" {
		payload := autosync.NativePayload{Name: "Live synchronization", Records: e.Records, Graph: map[string]any{}}
		for _, v := range e.Layout {
			status := "provisional_live_" + v.Status
			if strings.HasPrefix(v.Status, "unresolved") {
				status = v.Status
			}
			payload.Models = append(payload.Models, autosync.TimeModel{Recording: v.Recording, Component: v.Component, ComponentSize: v.ComponentSize, Offset: v.Start, Rate: v.Rate, Error: v.Error, Status: status, Knots: v.Knots, Resolved: !strings.HasPrefix(v.Status, "unresolved")})
		}
		a.media.SetProject(desktop.BuildProject(payload))
		a.log(e.Kind, fmt.Sprintf("%d recordings · %s", len(e.Records), e.Detail))
	}
}
