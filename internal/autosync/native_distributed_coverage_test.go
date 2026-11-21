package autosync

import (
	"reflect"
	"testing"
)

func TestLongReviewSearchUsesOnlySpareBudgetAndAcousticEvidence(t *testing.T) {
	records := []MediaRecord{{Duration: 7200}, {Duration: 35}, {Duration: 35}}
	layout := []NativeLayout{
		{Recording: 0, Status: "unresolved_disconnected_group"},
		{Recording: 1, Status: "unresolved_no_supported_relation"},
		{Recording: 2, Status: "unresolved_no_supported_relation"},
	}
	precise := &Measurement{Supported: true, Precise: true}
	weak := &Measurement{Supported: true}
	eligible := NativeProfile{
		Training: []*Measurement{precise, precise}, Selection: []*Measurement{weak, weak}, Audit: []*Measurement{weak, weak, precise},
	}
	ineligible := eligible
	ineligible.Audit = []*Measurement{weak, weak, weak}
	profiles := []NativeProfilePair{{Reference: 0, Query: 1, Profiles: []*NativeProfile{&eligible}}, {Reference: 0, Query: 2, Profiles: []*NativeProfile{&ineligible}}}
	if got := longReviewSearches(records, layout, profiles, nil, 7100); len(got) != 0 {
		t.Fatalf("review exceeded its budget: %v", got)
	}
	if got := longReviewSearches(records, layout, profiles, nil, 14400); !reflect.DeepEqual(got, [][2]int{{0, 1}}) {
		t.Fatalf("review search planned for wrong clips: %v", got)
	}
	if got := longReviewSearches(records, layout, profiles, map[[2]int]bool{{0, 1}: true}, 14400); len(got) != 0 {
		t.Fatalf("review repeated a scheduled search: %v", got)
	}
}

func TestLongReferenceCoverageBeforeAlternates(t *testing.T) {
	records := []MediaRecord{{Duration: 7200}, {Duration: 5000}, {Duration: 230}, {Duration: 25}}
	targets := []int{2, 3}
	refs := map[int][]int{2: {0, 1}, 3: {0, 1}}
	established := map[int]bool{0: true}
	cases := []struct {
		budget float64
		want   [][2]int
	}{
		{14400, [][2]int{{0, 2}, {0, 3}}},
		{19400, [][2]int{{0, 2}, {0, 3}, {1, 2}}},
		{12200, [][2]int{{0, 2}, {1, 2}}},
	}
	for _, tc := range cases {
		got := planLongReferenceCoverage(records, targets, refs, established, tc.budget)
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("budget %.0f: got %v want %v", tc.budget, got, tc.want)
		}
	}
}
