package dirsize

import "sync"

// queue is an unbounded LIFO work queue shared by a fixed set of workers.
// LIFO order keeps the walk roughly depth-first, which bounds the number of
// pending directories. pending counts queued plus in-progress jobs; when it
// drops to zero the walk is complete and every waiting worker is released.
type queue struct {
	mu      sync.Mutex
	cond    *sync.Cond
	jobs    []job
	pending int
	closed  bool
}

func newQueue() *queue {
	q := &queue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// push adds jobs. It must be called before done() for the job that
// discovered them so pending never reaches zero prematurely.
func (q *queue) push(jobs ...job) {
	q.mu.Lock()
	q.jobs = append(q.jobs, jobs...)
	q.pending += len(jobs)
	q.mu.Unlock()
	if len(jobs) == 1 {
		q.cond.Signal()
	} else {
		q.cond.Broadcast()
	}
}

// pop blocks until a job is available or the queue is closed (walk
// finished or canceled), in which case ok is false.
func (q *queue) pop() (j job, ok bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.jobs) == 0 && !q.closed {
		q.cond.Wait()
	}
	if q.closed {
		return job{}, false
	}
	last := len(q.jobs) - 1
	j = q.jobs[last]
	q.jobs[last] = job{} // drop references for the GC
	q.jobs = q.jobs[:last]
	return j, true
}

// done marks one popped job as finished.
func (q *queue) done() {
	q.mu.Lock()
	q.pending--
	finished := q.pending == 0
	if finished {
		q.closed = true
	}
	q.mu.Unlock()
	if finished {
		q.cond.Broadcast()
	}
}

// close releases all workers without waiting for pending jobs.
func (q *queue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.cond.Broadcast()
}
