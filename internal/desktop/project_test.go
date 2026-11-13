package desktop

import (
	"math"
	"opensync/internal/autosync"
	"testing"
)

func TestPreciseLayoutAndDisconnectedComponents(t *testing.T) {
	r := []autosync.MediaRecord{{ID: "a", Name: "a", Duration: 10, Source: "a.wav"}, {ID: "b", Name: "b", Duration: 4, Source: "b.wav"}, {ID: "c", Name: "c", Duration: 2, Source: "c.wav"}}
	m := []autosync.TimeModel{{Recording: 0, Component: 1, Rate: 1, Offset: -2.123456789, Resolved: true, Status: "acoustically_supported_not_certified"}, {Recording: 1, Component: 1, Rate: 1, Offset: 1.123456789, Resolved: true, Status: "provisional_comparison"}, {Recording: 2, Component: 2, Rate: 1, Offset: 0, Status: "unresolved_no_supported_relation"}}
	p := BuildProject(autosync.NativePayload{Records: r, Models: m})
	if p.Clips[0].Start != 0 || math.Abs(p.Clips[1].Start-3.246913578) > 1e-12 {
		t.Fatal("precision lost", p.Clips)
	}
	if p.Clips[0].Track == p.Clips[1].Track {
		t.Fatal("overlap assigned same lane")
	}
	if p.Clips[2].Start != 20 || p.Clips[0].State != "supported" || p.Clips[1].State != "provisional" || p.Clips[2].State != "unresolved" {
		t.Fatal("status/append policy changed", p.Clips)
	}
	if m[0].Offset != -2.123456789 {
		t.Fatal("mutated engine models")
	}
}
func TestPiecewiseMappingPreserved(t *testing.T) {
	points := []autosync.TimePoint{{0, -1}, {2, 1.00001}, {5, 4.0002}}
	p := BuildProject(autosync.NativePayload{Records: []autosync.MediaRecord{{ID: "x", Duration: 5, Source: "x"}}, Models: []autosync.TimeModel{{Recording: 0, Component: 1, Rate: 1, Knots: points, Resolved: true, Status: "supported"}}})
	if points[0][1] != -1 || math.Abs(p.Clips[0].Mapping[1][1]-2.00001) > 1e-12 || math.Abs(p.Duration-5.0002) > 1e-12 {
		t.Fatal("nonlinear mapping changed")
	}
}
func TestBoundedCache(t *testing.T) {
	c := newCache(10)
	c.put("a", make([]byte, 6))
	c.put("b", make([]byte, 6))
	if _, ok := c.get("a"); ok {
		t.Fatal("LRU not evicted")
	}
	if c.bytes > 10 {
		t.Fatal("budget exceeded")
	}
}
