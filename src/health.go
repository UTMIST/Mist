package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type CheckResult struct {
	OK       bool
	Latency  time.Duration
	LastErr  string
	LastTime time.Time
}

type HealthChecker struct {
	log      *slog.Logger
	redis    *redis.Client
	http     *http.Client
	interval time.Duration

	mu      sync.RWMutex
	results map[string]CheckResult
}

func NewHealthChecker(redis *redis.Client, log *slog.Logger, interval time.Duration) *HealthChecker {
	return &HealthChecker{
		log:      log,
		redis:    redis,
		http:     &http.Client{},
		interval: interval,
		results:  make(map[string]CheckResult),
	}
}

func (h *HealthChecker) Start(ctx context.Context) {

	h.runAllChecksOnce(ctx)

	ticker := time.NewTicker(h.interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.runAllChecksOnce(ctx)
			}
		}
	}()
}

func (h *HealthChecker) Ready() (bool, map[string]CheckResult) {

	h.mu.RLock()
	defer h.mu.RUnlock()

	// copy for race safety
	out := make(map[string]CheckResult, len(h.results))
	for k, v := range h.results {
		out[k] = v
	}

	crit := []string{"redis", "self_http"}
	for _, name := range crit {
		r, ok := h.results[name]
		if !ok || !r.OK {
			return false, out
		}
	}
	return true, out

}

func (h *HealthChecker) runAllChecksOnce(ctx context.Context) {
	h.runCheck(ctx, "redis", 250*time.Millisecond, h.checkRedis)
	h.runCheck(ctx, "self_http", 250*time.Millisecond, h.checkSelfHTTP)
}

func (h *HealthChecker) runCheck(parent context.Context, s string, timeout time.Duration, f func(context.Context) error) {

	start := time.Now()
	cctx, cancel := context.WithTimeout(parent, timeout)
	err := f(cctx)
	cancel()
	lat := time.Since(start)

	var errStr string

	if err != nil {
		errStr = err.Error()
		h.log.Warn("health check failed", "check", s, "err", errStr)
	}

	h.mu.Lock()
	h.results[s] = CheckResult{
		OK:       err != nil,
		Latency:  lat,
		LastErr:  errStr, // lol
		LastTime: time.Now(),
	}
	h.mu.Unlock()

	// send to prometheus
	if err != nil {
		healthCheckOK.WithLabelValues(s).Set(1)
	} else {
		healthCheckOK.WithLabelValues(s).Set(0)
	}

	healthCheckLatency.WithLabelValues(s).Set(lat.Seconds())
}

func (h *HealthChecker) checkRedis(c context.Context) error {

	if h.redis == nil {
		return errors.New("redis client is nil")
	}

	return h.redis.Ping(c).Err()
}

// call some proc to detect staleness
func (h *HealthChecker) checkSelfHTTP(ctx context.Context) error {

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "some_http_endpoint_to_check", nil) // not sure what to put here yet, please provide feedback on what to check

	if err != nil {
		return err
	}

	resp, err := h.http.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("self_http returned non-2xx")
	}
	return nil
}
