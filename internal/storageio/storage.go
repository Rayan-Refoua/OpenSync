// Package storageio coordinates source-media I/O by physical storage device.
// A rotating disk gets one active lease across all its volumes; confirmed
// non-rotating storage has no additional serialization. CPU processing should
// happen after releasing the lease, and callers must not nest leases.
package storageio

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	KindHDD     = "hdd"
	KindSSD     = "ssd"
	KindRAM     = "ram"
	KindUnknown = "unknown"
)

type Info struct {
	Kind       string   `json:"kind"`
	Volume     string   `json:"volume"`
	DiskIDs    []uint32 `json:"diskIDs"`
	GroupIDs   []string `json:"groupIDs"`
	Serialized bool     `json:"serialized"`
	Reason     string   `json:"reason"`
}

type GroupStats struct {
	ID      string `json:"id"`
	Active  int    `json:"active"`
	Waiting int    `json:"waiting"`
}

type volumeIdentity struct {
	key, root, device string
	driveType         uint32
	reason            string
}

type classification struct {
	info    Info
	expires time.Time
	touched uint64
}

type pendingClassification struct {
	done chan struct{}
	info Info
}

type group struct {
	id                    string
	semaphore             chan struct{}
	active, waiting, refs int
}

// Registry caches at most 128 classifications, expiring after two minutes so
// removable-device changes are re-evaluated. Limiter entries exist only while
// at least one active/waiting acquisition references them.
type Registry struct {
	mu       sync.Mutex
	cache    map[string]classification
	pending  map[string]*pendingClassification
	groups   map[string]*group
	tick     uint64
	capacity int
	resolve  func(string) volumeIdentity
	classify func(volumeIdentity) Info
}

func NewRegistry() *Registry {
	return &Registry{cache: map[string]classification{}, pending: map[string]*pendingClassification{}, groups: map[string]*group{}, capacity: 128, resolve: resolveVolume, classify: classifyVolume}
}

var shared = NewRegistry()

func Inspect(path string) Info { return shared.Inspect(path) }

func Acquire(ctx context.Context, path string) (func(), error) {
	return shared.Acquire(ctx, path)
}

func Snapshot() []GroupStats { return shared.Snapshot() }

func CachedVolumes() []Info { return shared.CachedVolumes() }

func cloneInfo(info Info) Info {
	info.DiskIDs = append([]uint32(nil), info.DiskIDs...)
	info.GroupIDs = append([]string(nil), info.GroupIDs...)
	return info
}

func (r *Registry) Inspect(path string) Info {
	v := r.resolve(path)
	if v.key == "" {
		v.key = "unknown-volume"
	}
	r.mu.Lock()
	r.tick++
	if c, ok := r.cache[v.key]; ok && time.Now().Before(c.expires) {
		c.touched = r.tick
		r.cache[v.key] = c
		r.mu.Unlock()
		info := cloneInfo(c.info)
		info.Volume = v.root
		return info
	}
	if p, ok := r.pending[v.key]; ok {
		r.mu.Unlock()
		<-p.done
		info := cloneInfo(p.info)
		info.Volume = v.root
		return info
	}
	p := &pendingClassification{done: make(chan struct{})}
	r.pending[v.key] = p
	r.mu.Unlock()
	info := r.classify(v)
	info.Volume = v.root
	info.GroupIDs = uniqueStrings(info.GroupIDs)
	info.Serialized = len(info.GroupIDs) > 0
	r.mu.Lock()
	r.tick++
	r.cache[v.key] = classification{info: cloneInfo(info), expires: time.Now().Add(2 * time.Minute), touched: r.tick}
	for len(r.cache) > r.capacity {
		oldestKey := ""
		oldest := ^uint64(0)
		for key, c := range r.cache {
			if c.touched < oldest {
				oldest, oldestKey = c.touched, key
			}
		}
		delete(r.cache, oldestKey)
	}
	p.info = cloneInfo(info)
	delete(r.pending, v.key)
	close(p.done)
	r.mu.Unlock()
	return cloneInfo(info)
}

func (r *Registry) Acquire(ctx context.Context, path string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info := r.Inspect(path)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !info.Serialized {
		return func() {}, nil
	}
	// Canonical ordering prevents deadlock for spanned/mirrored volumes.
	ids := uniqueStrings(info.GroupIDs)
	r.mu.Lock()
	groups := make([]*group, 0, len(ids))
	for _, id := range ids {
		g := r.groups[id]
		if g == nil {
			g = &group{id: id, semaphore: make(chan struct{}, 1)}
			r.groups[id] = g
		}
		g.refs++
		groups = append(groups, g)
	}
	r.mu.Unlock()
	acquired := 0
	releaseAll := func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		for i := acquired - 1; i >= 0; i-- {
			g := groups[i]
			<-g.semaphore
			g.active--
		}
		for _, g := range groups {
			g.refs--
			if g.refs == 0 {
				delete(r.groups, g.id)
			}
		}
	}
	for _, g := range groups {
		r.mu.Lock()
		g.waiting++
		r.mu.Unlock()
		select {
		case g.semaphore <- struct{}{}:
			r.mu.Lock()
			g.waiting--
			g.active++
			r.mu.Unlock()
			acquired++
			if err := ctx.Err(); err != nil {
				releaseAll()
				return nil, err
			}
		case <-ctx.Done():
			r.mu.Lock()
			g.waiting--
			r.mu.Unlock()
			releaseAll()
			return nil, ctx.Err()
		}
	}
	var once sync.Once
	return func() { once.Do(releaseAll) }, nil
}

func (r *Registry) Snapshot() []GroupStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]GroupStats, 0, len(r.groups))
	for id, g := range r.groups {
		out = append(out, GroupStats{ID: id, Active: g.active, Waiting: g.waiting})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (r *Registry) CachedVolumes() []Info {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Info, 0, len(r.cache))
	for _, c := range r.cache {
		if time.Now().Before(c.expires) {
			out = append(out, cloneInfo(c.info))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Volume < out[j].Volume })
	return out
}

func uniqueStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	n := 0
	for _, s := range out {
		if s != "" && (n == 0 || out[n-1] != s) {
			out[n] = s
			n++
		}
	}
	return out[:n]
}

func unknownVolume(v volumeIdentity, reason string) Info {
	return Info{Kind: KindUnknown, Volume: v.root, GroupIDs: []string{"volume:" + strings.ToLower(v.key)}, Serialized: true, Reason: reason}
}
