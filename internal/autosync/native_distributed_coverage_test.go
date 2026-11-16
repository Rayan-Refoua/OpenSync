package autosync

import (
	"reflect"
	"testing"
)

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
