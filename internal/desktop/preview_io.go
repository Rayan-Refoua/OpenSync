package desktop

import (
	"context"
	"opensync/internal/storageio"
	"sync"
)

// Waveforms and thumbnails have separate throughput samples because a
// completed waveform chunk and a completed thumbnail represent different work.
type previewIO struct {
	mu      sync.Mutex
	devices map[string]*storageio.IOProbe
}

func (p *previewIO) acquire(ctx context.Context, source string) (func(bool), error) {
	key := storageio.IOProbeKey(source)
	if key == "" {
		return func(bool) {}, nil
	}
	p.mu.Lock()
	if p.devices == nil {
		p.devices = make(map[string]*storageio.IOProbe)
	}
	probe := p.devices[key]
	if probe == nil && len(p.devices) < 128 {
		probe = storageio.NewPreviewProbe()
		p.devices[key] = probe
	}
	p.mu.Unlock()
	if probe == nil {
		return func(bool) {}, nil
	}
	release, err := probe.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	return func(ok bool) {
		if ok {
			probe.Observe(1)
		}
		release()
	}, nil
}
