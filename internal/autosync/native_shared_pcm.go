package autosync

import "context"

type pcmBlockRequest struct {
	done  chan struct{}
	ctx   context.Context
	count int64
	pcm   []int16
	err   error
}

// All callers for a compressed block share its decode, even when the cache is
// full. One queue per source drains requests already waiting in sample order.
func (d *directAudio) sharedBlock(ctx context.Context, r MediaRecord, first, count int64) ([]int16, error) {
	key := audioWindowKey(r.ID, first)
	d.mu.Lock()
	if pcm := d.clips[key]; pcm != nil {
		d.tick++
		d.age[key] = d.tick
		d.mu.Unlock()
		return pcm, nil
	}
	if d.blockRequests == nil {
		d.blockRequests = make(map[string]map[int64]*pcmBlockRequest)
	}
	queue := d.blockRequests[r.ID]
	start := queue == nil
	if start {
		queue = make(map[int64]*pcmBlockRequest)
		d.blockRequests[r.ID] = queue
	}
	request := queue[first]
	if request == nil {
		request = &pcmBlockRequest{done: make(chan struct{}), ctx: ctx, count: count}
		queue[first] = request
	} else {
		d.sharedReads.Add(1)
	}
	d.mu.Unlock()
	if start {
		go d.drainSourceBlocks(r, queue)
	}
	select {
	case <-ctx.Done():
		// Let in-flight decoding exit before the caller can close the decoder pool.
		<-request.done
		return nil, ctx.Err()
	case <-request.done:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return request.pcm, request.err
	}
}

func (d *directAudio) drainSourceBlocks(r MediaRecord, queue map[int64]*pcmBlockRequest) {
	for {
		d.mu.Lock()
		var first int64
		var request *pcmBlockRequest
		for position, queued := range queue {
			if request == nil || position < first {
				first, request = position, queued
			}
		}
		if request == nil {
			delete(d.blockRequests, r.ID)
			d.mu.Unlock()
			return
		}
		d.mu.Unlock()
		key := audioWindowKey(r.ID, first)
		request.pcm = d.window(key)
		if request.pcm == nil {
			session, err := d.acquire(request.ctx, r.ID)
			request.err = err
			if err == nil {
				d.misses.Add(1)
				d.reading.Add(1)
				request.pcm, request.err = session.read(request.ctx, r, first, request.count, &d.starts)
				d.reading.Add(-1)
				d.decoders <- session
				if request.err == nil {
					d.retain(key, request.pcm)
				}
			}
		}
		d.mu.Lock()
		delete(queue, first)
		close(request.done)
		d.mu.Unlock()
	}
}
