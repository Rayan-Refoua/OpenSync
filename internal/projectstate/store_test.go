package projectstate

import (
	"os"
	"path/filepath"
	"testing"
)

func storedFixture(t *testing.T) *Project {
	t.Helper()
	p := fixture(t)

	if err := p.AddHypothesis(Hypothesis{
		ID: "h1", Reference: "A", Query: "B",
		OffsetNS: 12_000_000_000,
		Status:   Discovered,
	}); err != nil {
		t.Fatal(err)
	}

	if err := p.AddObservation(Observation{
		ID: "o1", HypothesisID: "h1",
		EventID: "event-1", Feature: "landmark",
		ReferenceNS: 15_000_000_000,
		QueryNS:     3_000_000_000,
	}); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProjectRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project.json")
	original := storedFixture(t)

	if err := Save(path, original); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(loaded.Recordings) != 2 ||
		len(loaded.Hypotheses) != 1 ||
		len(loaded.Observations) != 1 {
		t.Fatal("evidence lost during round trip")
	}

	if loaded.Hypotheses["h1"].OffsetNS != 12_000_000_000 {
		t.Fatal("offset changed during round trip")
	}
}

func TestInvalidSavePreservesOriginal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project.json")
	p := storedFixture(t)

	if err := Save(path, p); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	p.Observations["o1"] = Observation{
		ID: "o1", HypothesisID: "missing",
		EventID: "event", Feature: "landmark",
	}
	if err := Save(path, p); err == nil {
		t.Fatal("invalid project saved")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("existing project changed on failed save")
	}
}

func TestCorruptProjectRejected(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string]string{
		"truncated": `{"version":1,`,
		"schema":    `{"version":99,"recordings":{},"hypotheses":{},"observations":{}}`,
		"trailing":  `{} {}`,
		"unknown":   `{"version":1,"recordings":{},"hypotheses":{},"observations":{},"unexpected":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name+".json")
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("invalid project accepted")
			}
		})
	}
}

func TestOutOfBoundsObservationRejected(t *testing.T) {
	p := storedFixture(t)
	o := p.Observations["o1"]
	o.ReferenceNS = 121_000_000_000
	p.Observations["o1"] = o

	if err := p.Validate(); err == nil {
		t.Fatal("out-of-bounds observation accepted")
	}
}
