package jobentry

import (
	"net"
	"testing"
	"time"
)

func TestAwaitIsolation(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	open := l.Addr().String()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	if err := AwaitIsolation([]string{open}, time.Second); err == nil {
		t.Fatal("reachable target passed the gate")
	}
	l.Close() // now refused, as REJECT rules make it
	start := time.Now()
	if err := AwaitIsolation([]string{open}, 5*time.Second); err != nil {
		t.Fatalf("blocked target failed the gate: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("gate slow on a blocked target")
	}
}
