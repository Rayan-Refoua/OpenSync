package main

import (
	"context"
	"embed"
	"fmt"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"io/fs"
	"os"
	"slices"
)

//go:embed all:frontend
var assets embed.FS

func main() {
	a := newApp()
	files, _ := fs.Sub(assets, "frontend")
	err := wails.Run(&options.App{
		Title: "OpenSync — Synchronization workstation", StartHidden: os.Getenv("OPENSYNC_DESKTOP_SMOKE") != "",
		Width: 1500, Height: 930, MinWidth: 1000, MinHeight: 640,
		BackgroundColour: options.NewRGB(19, 22, 26),
		AssetServer:      &assetserver.Options{Assets: files, Handler: a.media.Handler()},
		OnStartup:        a.startup,
		OnShutdown:       func(context.Context) { a.CancelSync(); a.media.Close() },
		Bind:             []interface{}{a},
		Debug:            options.Debug{OpenInspectorOnStartup: slices.Contains(os.Args[1:], "--debug-ui") && os.Getenv("OPENSYNC_DESKTOP_SMOKE") == ""},
		Windows:          &windows.Options{Theme: windows.Dark},
		DragAndDrop:      &options.DragAndDrop{EnableFileDrop: true},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
