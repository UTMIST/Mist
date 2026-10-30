package jobentry

import (
	"fmt"
	"net"
	"time"
)

// AwaitIsolation blocks until every target refuses or drops connections
// several times in a row, proving the job's network filtering is in place.
// Kubernetes policy controllers program a Pod's rules only after it
// starts, so user code must not run until this passes. Targets must be
// services known to be listening (the node's kubelet is: it is starting
// this Pod), so a failed connection means filtering, not an absent service.
func AwaitIsolation(targets []string, timeout time.Duration) error {
	const needBlocked = 3
	deadline := time.Now().Add(timeout)
	blocked := 0
	for blocked < needBlocked {
		if time.Now().After(deadline) {
			return fmt.Errorf("network isolation not in place after %s: %v still reachable", timeout, targets)
		}
		open := false
		for _, t := range targets {
			if c, err := net.DialTimeout("tcp", t, time.Second); err == nil {
				c.Close()
				open = true
			}
		}
		if open {
			blocked = 0
			time.Sleep(200 * time.Millisecond)
		} else {
			blocked++
		}
	}
	return nil
}
