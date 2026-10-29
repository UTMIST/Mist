package clock

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

func TestFakeFiresInTimeOrder(t *testing.T) {
	c := NewFake(t0)
	var got []string
	c.AfterFunc(3*time.Second, func() { got = append(got, "c") })
	c.AfterFunc(time.Second, func() {
		got = append(got, "a")
		// Created during Advance and due before the target: fires too.
		c.AfterFunc(time.Second, func() { got = append(got, "b@"+c.Now().Sub(t0).String()) })
	})
	stopped := c.AfterFunc(2*time.Second, func() { got = append(got, "never") })
	if !stopped.Stop() || stopped.Stop() {
		t.Fatal("Stop should succeed once")
	}
	c.Advance(5 * time.Second)
	want := []string{"a", "b@2s", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if !c.Now().Equal(t0.Add(5 * time.Second)) {
		t.Fatalf("now = %v", c.Now())
	}
}

func TestFakeTimerChannel(t *testing.T) {
	c := NewFake(t0)
	tm := c.NewTimer(time.Minute)
	c.Advance(59 * time.Second)
	select {
	case <-tm.C():
		t.Fatal("fired early")
	default:
	}
	c.Advance(time.Second)
	if at := <-tm.C(); !at.Equal(t0.Add(time.Minute)) {
		t.Fatalf("fired at %v", at)
	}
	if c.Pending() != 0 {
		t.Fatal("fired timer still pending")
	}
}

func TestFakeRefusesToRewind(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("want panic")
		}
	}()
	NewFake(t0).Set(t0.Add(-time.Second))
}
