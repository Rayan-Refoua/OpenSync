package autosync

import (
	"runtime"
	"sync"
	"sync/atomic"
)

// All pairs submit independent leaf work to one bounded pool. A worker never
// submits more work to this pool, so parent tasks cannot exhaust its workers
// while waiting for children. Results are written to preassigned indices.
type nativeComputePool struct {
	jobs                    chan nativeComputeJob
	workers                 sync.WaitGroup
	active, peak, completed atomic.Int64
	limit                   int
}
type nativeComputeJob struct {
	run  func()
	done *sync.WaitGroup
}

func (a *NativeAnalyzer) computeTasks(count int, work func(int)) {
	if count == 0 {
		return
	}
	a.computeOnce.Do(func() {
		n := nativeWorkers(runtime.GOMAXPROCS(0))
		p := &nativeComputePool{jobs: make(chan nativeComputeJob, 2*n), limit: n}
		a.compute = p
		a.computeStats.Store(p)
		for i := 0; i < n; i++ {
			p.workers.Add(1)
			go func() {
				defer p.workers.Done()
				for task := range p.jobs {
					active := p.active.Add(1)
					for old := p.peak.Load(); active > old; old = p.peak.Load() {
						if p.peak.CompareAndSwap(old, active) {
							break
						}
					}
					task.run()
					p.active.Add(-1)
					p.completed.Add(1)
					task.done.Done()
				}
			}()
		}
	})
	var done sync.WaitGroup
	done.Add(count)
	for i := 0; i < count; i++ {
		index := i
		a.compute.jobs <- nativeComputeJob{func() { work(index) }, &done}
	}
	done.Wait()
}
func (p *nativeComputePool) close() { close(p.jobs); p.workers.Wait() }
