package executor

import "sync"

// Queue is an unbounded event queue in front of an unbuffered channel, so
// an executor never blocks on a slow consumer. Delivery is at-least-once
// from the consumer's point of view only because executors may re-emit;
// the queue itself neither drops nor duplicates.
type Queue struct {
	ch   chan Event
	done chan struct{}

	mu     sync.Mutex
	cond   *sync.Cond
	items  []Event
	closed bool
}

func NewQueue() *Queue {
	q := &Queue{ch: make(chan Event), done: make(chan struct{})}
	q.cond = sync.NewCond(&q.mu)
	go q.pump()
	return q
}

func (q *Queue) C() <-chan Event { return q.ch }

func (q *Queue) Push(ev Event) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, ev)
	q.cond.Signal()
}

func (q *Queue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.closed {
		q.closed = true
		close(q.done)
		q.cond.Signal()
	}
}

func (q *Queue) pump() {
	for {
		q.mu.Lock()
		for len(q.items) == 0 && !q.closed {
			q.cond.Wait()
		}
		if q.closed {
			q.mu.Unlock()
			return
		}
		ev := q.items[0]
		q.items = q.items[1:]
		q.mu.Unlock()
		select {
		case q.ch <- ev:
		case <-q.done:
			return
		}
	}
}
