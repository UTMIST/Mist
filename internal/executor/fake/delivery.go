package fake

import (
	"slices"
	"sync"

	"github.com/UTMIST/Mist/internal/executor"
)

// delivery is an unbounded, controllable notification queue in front of an
// unbuffered channel, so emitting never blocks the executor.
type delivery struct {
	ch   chan executor.Event
	done chan struct{}

	mu        sync.Mutex
	cond      *sync.Cond
	queue     []executor.Event
	held      []executor.Event
	holding   bool
	duplicate bool
	closed    bool
}

func newDelivery() *delivery {
	d := &delivery{ch: make(chan executor.Event), done: make(chan struct{})}
	d.cond = sync.NewCond(&d.mu)
	go d.pump()
	return d
}

func (d *delivery) pump() {
	for {
		d.mu.Lock()
		for len(d.queue) == 0 && !d.closed {
			d.cond.Wait()
		}
		if d.closed {
			d.mu.Unlock()
			return
		}
		ev := d.queue[0]
		d.queue = d.queue[1:]
		d.mu.Unlock()
		select {
		case d.ch <- ev:
		case <-d.done:
			return
		}
	}
}

func (d *delivery) push(ev executor.Event) {
	d.mu.Lock()
	defer d.mu.Unlock()
	evs := []executor.Event{ev}
	if d.duplicate {
		evs = append(evs, ev)
	}
	if d.holding {
		d.held = append(d.held, evs...)
		return
	}
	d.queue = append(d.queue, evs...)
	d.cond.Signal()
}

func (d *delivery) hold() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.holding = true
}

func (d *delivery) release(reversed bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if reversed {
		slices.Reverse(d.held)
	}
	d.queue = append(d.queue, d.held...)
	d.held, d.holding = nil, false
	d.cond.Signal()
}

func (d *delivery) dropHeld() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.held, d.holding = nil, false
}

// dropQueued loses every notification not yet handed to the channel. One
// event already taken by the pump may still arrive: delivery is
// at-least-once, never exactly-once.
func (d *delivery) dropQueued() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.queue, d.held = nil, nil
}

func (d *delivery) setDuplicate(on bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.duplicate = on
}

func (d *delivery) close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.closed {
		d.closed = true
		close(d.done)
		d.cond.Signal()
	}
}
