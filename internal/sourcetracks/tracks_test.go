package sourcetracks

import (
	"reflect"
	"testing"

	"opensync/internal/projectstate"
)

func fixture(t *testing.T) *projectstate.Project {
	t.Helper()
	p := projectstate.New()
	for _, rec := range []projectstate.Recording{
		{ID: "a2", SourcePath: `I:\Shoot\CameraA\002.MP4`, DurationNS: 10_000_000_000},
		{ID: "b1", SourcePath: `I:\Shoot\CameraB\001.MP4`, DurationNS: 10_000_000_000},
		{ID: "a1", SourcePath: `I:\Shoot\CameraA\001.MP4`, DurationNS: 10_000_000_000},
	} {
		if err := p.AddRecording(rec); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestProposesTracksWithoutConfirmingDevices(t *testing.T) {
	proposal, err := Build(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.Tracks) != 2 || len(proposal.Neighbors) != 1 {
		t.Fatalf("unexpected proposal: %+v", proposal)
	}
	if proposal.Tracks[0].DeviceConfirmed ||
		proposal.Tracks[1].DeviceConfirmed {
		t.Fatal("directory was incorrectly certified as a device")
	}
	if !reflect.DeepEqual(
		proposal.Tracks[0].RecordingIDs, []string{"a1", "a2"},
	) {
		t.Fatalf("unexpected members: %+v", proposal.Tracks[0])
	}
	if proposal.Neighbors[0].Continuity != "unknown" ||
		proposal.Neighbors[0].Basis != "filename_sort_only" {
		t.Fatal("filename adjacency was incorrectly certified")
	}
}

func TestProposalDeterministic(t *testing.T) {
	p := fixture(t)
	first, err := Build(p)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("proposal changed between runs")
	}
}

func TestNoMutationOfProject(t *testing.T) {
	p := fixture(t)
	before := len(p.Hypotheses)
	if _, err := Build(p); err != nil {
		t.Fatal(err)
	}
	if len(p.Hypotheses) != before {
		t.Fatal("track proposal modified acoustic hypotheses")
	}
}
