package quality

import "testing"

func TestModesHaveIncreasingSamplingDepth(t *testing.T) {
	d, err := Parse("draft")
	if err != nil {
		t.Fatal(err)
	}
	n, err := Parse("normal")
	if err != nil {
		t.Fatal(err)
	}
	r, err := Parse("sock-solid")
	if err != nil {
		t.Fatal(err)
	}
	if !(len(d.ProbeFractions) < len(n.ProbeFractions) &&
		len(n.ProbeFractions) < len(r.ProbeFractions)) {
		t.Fatal("inconsistent effort ordering")
	}
	if r.Mode != RockSolid || r.HeldOutChecks <= n.HeldOutChecks {
		t.Fatalf("wrong rock solid preset: %+v", r)
	}
	if _, err := Parse("reckless"); err == nil {
		t.Fatal("unknown preset accepted")
	}
}

func TestValidControlsUseBoundsAndFallback(t *testing.T) {
	offsets := []int64{-15, 15, -5, 5, -30, 30}
	got := ValidControls(30*16000, 0, 10_000_000_000,
		8_000_000_000, 500_000_000, offsets)
	if len(got) != 2 || got[0] != 25_000_000_000 ||
		got[1] != 5_000_000_000 {
		t.Fatalf("wrong valid controls: %v", got)
	}
}

func TestMissingControlsAreExplicit(t *testing.T) {
	got := ValidControls(9*16000, 0, 0,
		8_000_000_000, 500_000_000, []int64{-15, 15})
	if len(got) != 0 {
		t.Fatalf("invalid controls were selected: %v", got)
	}
}
