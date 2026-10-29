// Package clock lets code that waits or reads the time run on virtual time
// in tests and the simulator. Nothing in mistd calls time.Now or time.After
// directly outside this package.
package clock

import "time"

type Clock interface {
	Now() time.Time
	NewTimer(d time.Duration) Timer
}

type Timer interface {
	C() <-chan time.Time
	Stop() bool
}

// Real is the wall clock.
type Real struct{}

func (Real) Now() time.Time { return time.Now() }

func (Real) NewTimer(d time.Duration) Timer { return realTimer{time.NewTimer(d)} }

type realTimer struct{ t *time.Timer }

func (r realTimer) C() <-chan time.Time { return r.t.C }
func (r realTimer) Stop() bool          { return r.t.Stop() }
