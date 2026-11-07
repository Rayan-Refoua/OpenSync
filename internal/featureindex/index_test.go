package featureindex

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestRealCacheIndexRoundtrip(t *testing.T) {
	source := os.Getenv("OPENSYNC_TEST_WAV")
	if source == "" {
		t.Skip("set OPENSYNC_TEST_WAV for integration test")
	}

	destination := filepath.Join(t.TempDir(), "test.osfi")
	if err := Build(source, destination); err != nil {
		t.Fatal(err)
	}

	idx, err := Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	if idx.Header.Count < 2 {
		t.Fatal("unexpectedly empty feature index")
	}
	valid, err := idx.ValidFor(source)
	if err != nil || !valid {
		t.Fatalf("source validation failed: %v", err)
	}

	first, err := idx.ReadAt(0)
	if err != nil {
		t.Fatal(err)
	}
	last, err := idx.ReadAt(idx.Header.Count - 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.CenterSample != 256 ||
		last.CenterSample <= first.CenterSample {
		t.Fatal("incorrect absolute sample coordinates")
	}
	if _, err := idx.ReadAt(idx.Header.Count); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestIndexDetectsCorruption(t *testing.T) {
	source := os.Getenv("OPENSYNC_TEST_WAV")
	if source == "" {
		t.Skip("set OPENSYNC_TEST_WAV for integration test")
	}

	destination := filepath.Join(t.TempDir(), "test.osfi")
	if err := Build(source, destination); err != nil {
		t.Fatal(err)
	}

	f, err := os.OpenFile(destination, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteAt([]byte{255}, HeaderSize+3)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if idx, err := Open(destination); err == nil {
		idx.Close()
		t.Fatal("corrupted index was accepted")
	}
}
