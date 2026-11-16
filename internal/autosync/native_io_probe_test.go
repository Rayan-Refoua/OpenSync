package autosync

import (
	"context"
	"testing"
	"time"
)

func TestIOProbeAdmissionAfterTrial(t *testing.T) {
	for _, tc := range []struct {
		name  string
		bytes int
		want  int
	}{
		{"slower", 10 << 20, 1}, {"faster", 30 << 20, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newIOProbe()
			a, err := p.acquire(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer a()
			p.started = time.Now().Add(-2 * time.Second)
			p.observe(2 << 20)
			b, err := p.acquire(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			p.started = time.Now().Add(-20 * time.Second)
			p.observe(tc.bytes)
			if p.limit != tc.want || p.active != 2 {
				t.Fatalf("limit=%d active=%d", p.limit, p.active)
			}
			b()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()
			c, err := p.acquire(ctx)
			if tc.want == 1 {
				if err != context.DeadlineExceeded {
					t.Fatalf("second reader was admitted: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				c()
			}
		})
	}
}
