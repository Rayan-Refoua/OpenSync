package desktop

import (
	"reflect"
	"testing"
)

func TestAuditionFallbackSwitchesWithinBlock(t *testing.T) {
	p := &Project{
		Clips: []Clip{
			{Start: 0, End: 1, Audio: true},
			{Start: 3, End: 4, Audio: true},
			{Start: 1, End: 2, Audio: true},
			{Start: 0, End: 4, Audio: true},
			{Start: 2.5, End: 4, Audio: true},
			{Start: 0, End: 4, Audio: true},
		},
		Tracks: []Track{
			{Clips: []int{0, 1}}, {Clips: []int{2}},
			{Kind: "audio", Clips: []int{3}},
			{Kind: "audio", Clips: []int{4}},
			{Kind: "video", Clips: []int{5}},
		},
	}
	got := planAudition(p, []int{0, 1}, 0, 4, true)
	want := []auditionClip{
		{0, []auditionSpan{{0, 1, .5}}},
		{2, []auditionSpan{{1, 2, .5}}},
		{3, []auditionSpan{{2, 3, .5}}},
		{5, []auditionSpan{{2, 2.5, .5}}},
		{4, []auditionSpan{{2.5, 3, .5}}},
		{1, []auditionSpan{{3, 4, .5}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fallback mix = %#v, want %#v", got, want)
	}
	got = planAudition(p, []int{0, 1}, 0, 4, false)
	want = []auditionClip{want[0], want[5], want[1]}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("manual mix = %#v, want %#v", got, want)
	}
}
