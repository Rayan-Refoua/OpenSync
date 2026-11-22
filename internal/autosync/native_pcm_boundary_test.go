package autosync

import "testing"

func TestPCMWindowBoundaryRounding(t *testing.T) {
	for _, tt := range []struct {
		name        string
		length      float64
		total, want int64
	}{
		{"endpoint fits", 20.6, 31, 20},
		{"interior keeps existing rounding", 20.6, 100, 21},
		{"genuine out-of-bounds stays invalid", 20.95, 31, 21},
	} {
		t.Run(tt.name, func(t *testing.T) {
			first, count := pcmWindowFrames(10.6/16000, tt.length/16000, tt.total)
			if first != 11 || count != tt.want {
				t.Fatalf("got (%d, %d), want (11, %d)", first, count, tt.want)
			}
		})
	}
}
