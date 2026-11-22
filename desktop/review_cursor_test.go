package main

import (
	"path/filepath"
	"testing"
)

func TestActiveReviewSurvivesReopenAndUndo(t *testing.T) {
	base := filepath.Join(t.TempDir(), "OpenSync.opensync")
	first := filepath.Join(filepath.Dir(base), "Solutions", "solution-first.opensync")
	second := filepath.Join(filepath.Dir(first), "solution-second.opensync")
	for _, want := range []string{first, second, first, base} {
		if err := saveActiveReview(base, want); err != nil {
			t.Fatal(err)
		}
		got, err := activeReviewPath(base)
		if err != nil || got != want {
			t.Fatalf("reopen = %q, %v; want %q", got, err, want)
		}
	}
}
