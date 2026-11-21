package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDroppedProjectPath(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "Session with spaces")
	empty := filepath.Join(root, "Media folder")
	for _, path := range []string{folder, empty} {
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	project := filepath.Join(folder, "OpenSync.opensync")
	revision := filepath.Join(folder, "Review.OPENSYNC")
	media := filepath.Join(folder, "Camera.mp4")
	for _, path := range []string{project, revision, media} {
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name  string
		paths []string
		want  string
	}{
		{"project folder", []string{folder}, project},
		{"project file", []string{project}, project},
		{"renamed project file", []string{revision}, revision},
		{"missing project in folder", []string{empty}, ""},
		{"media file", []string{media}, ""},
		{"multiple items", []string{project, folder}, ""},
		{"no items", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := droppedProjectPath(tc.paths)
			if got != tc.want || (err != nil) != (tc.want == "") {
				t.Fatalf("got (%q, %v), want %q", got, err, tc.want)
			}
		})
	}
}
