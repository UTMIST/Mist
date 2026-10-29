// Package clock lets code that waits or reads the time run on virtual time
// in tests and the simulator. Nothing in mistd calls time.Now or time.After
// directly outside this package.
package clock

import "time"

type Clock interface {
	Now() time.Time
	NewTimer(d time.Duration) Timer
	// AfterFunc calls f after d. On a Fake clock, f runs synchronously
	// inside Advance, in time order. The returned Timer's C is nil.
	AfterFunc(d time.Duration, f func()) Timer
}

type Timer interface {
	C() <-chan time.Time
	Stop() bool
}

// Real is the wall clock.
type Real struct{}

func (Real) Now() time.Time { return time.Now() }

func (Real) NewTimer(d time.Duration) Timer { return realTimer{time.NewTimer(d)} }

func (Real) AfterFunc(d time.Duration, f func()) Timer {
	return realTimer{time.AfterFunc(d, f)}
}

type realTimer struct{ t *time.Timer }

func (r realTimer) C() <-chan time.Time { return r.t.C }
func (r realTimer) Stop() bool          { return r.t.Stop() }
