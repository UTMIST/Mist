package clock

import (
	"slices"
	"sync"
	"time"
)

// Fake is a virtual clock. Time moves only when Advance or Set is called.
// Timers due at the same instant fire in creation order.
type Fake struct {
	mu     sync.Mutex
	now    time.Time
	seq    int64
	timers []*fakeTimer
}

// NewFake returns a fake clock starting at start.
func NewFake(start time.Time) *Fake { return &Fake{now: start} }

type fakeTimer struct {
	clock *Fake
	when  time.Time
	seq   int64
	ch    chan time.Time // NewTimer
	fn    func()         // AfterFunc
}

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *Fake) NewTimer(d time.Duration) Timer {
	return f.add(d, make(chan time.Time, 1), nil)
}

func (f *Fake) AfterFunc(d time.Duration, fn func()) Timer {
	return f.add(d, nil, fn)
}

func (f *Fake) add(d time.Duration, ch chan time.Time, fn func()) *fakeTimer {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	t := &fakeTimer{clock: f, when: f.now.Add(d), seq: f.seq, ch: ch, fn: fn}
	f.timers = append(f.timers, t)
	return t
}

func (t *fakeTimer) C() <-chan time.Time { return t.ch }

func (t *fakeTimer) Stop() bool {
	f := t.clock
	f.mu.Lock()
	defer f.mu.Unlock()
	i := slices.Index(f.timers, t)
	if i < 0 {
		return false
	}
	f.timers = slices.Delete(f.timers, i, i+1)
	return true
}

// Advance moves time forward by d, firing every timer that comes due on the
// way, including timers created by callbacks it runs.
func (f *Fake) Advance(d time.Duration) { f.Set(f.Now().Add(d)) }

// Set moves time forward to t. Moving backwards panics: virtual time never
// rewinds, just as Mist assumes monotonic time within a boot.
func (f *Fake) Set(target time.Time) {
	for {
		f.mu.Lock()
		if target.Before(f.now) {
			f.mu.Unlock()
			panic("clock: Fake moved backwards")
		}
		next := f.nextDue(target)
		if next == nil {
			f.now = target
			f.mu.Unlock()
			return
		}
		f.timers = slices.DeleteFunc(f.timers, func(t *fakeTimer) bool { return t == next })
		f.now = next.when
		f.mu.Unlock()

		if next.fn != nil {
			next.fn()
		} else {
			select {
			case next.ch <- next.when:
			default:
			}
		}
	}
}

func (f *Fake) nextDue(target time.Time) *fakeTimer {
	var best *fakeTimer
	for _, t := range f.timers {
		if t.when.After(target) {
			continue
		}
		if best == nil || t.when.Before(best.when) || (t.when.Equal(best.when) && t.seq < best.seq) {
			best = t
		}
	}
	return best
}

// Pending returns how many timers are waiting. Tests use it to know that
// a goroutine has reached its wait before advancing time.
func (f *Fake) Pending() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.timers)
}
