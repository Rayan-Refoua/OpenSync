//go:build !windows

package main

import (
	"context"
	wr "github.com/wailsapp/wails/v2/pkg/runtime"
)

func selectMediaFolders(ctx context.Context) ([]string, error) {
	path, err := wr.OpenDirectoryDialog(ctx, wr.OpenDialogOptions{Title: "Add media folder (recursive)"})
	if err != nil || path == "" {
		return nil, err
	}
	return []string{path}, nil
}
