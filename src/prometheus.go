package main

import (
	"context"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	cpuinfo "github.com/shirou/gopsutil/v3/cpu"
	diskinfo "github.com/shirou/gopsutil/v3/disk"
	meminfo "github.com/shirou/gopsutil/v3/mem"
)

func handle() {
	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(":2112", nil)
}

// http metrics
var (
	httpInFlight = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "http_in_flight_requests",
		Help: "Current number of in-flight HTTP requests.",
	})
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total HTTP requests by handler/method/status.",
	}, []string{"handler", "method", "code"})
	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"handler", "method", "code"})
)

// job metrics
var (
	jobsStarted = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "jobs_started_total",
		Help: "Total number of jobs started.",
	}, []string{"job_type", "gpu"})
	jobsCompleted = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "jobs_completed_total",
		Help: "Total number of jobs completed successfully.",
	}, []string{"job_type", "gpu"})
	jobsFailed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "jobs_failed_total",
		Help: "Total number of jobs that failed.",
	}, []string{"job_type", "gpu"})
	runningJobs = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "running_jobs",
		Help: "Total jobs currently running",
	}, []string{"gpu"})
)

// system metrics (cpu, memory, disk, etc.)
var (
	// cpu (percent of total)
	systemCPUPercent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "system_cpu_usage_percent",
		Help: "Host CPU usage percentage (all cores averaged).",
	})

	// memory
	systemMemTotalBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "system_memory_total_bytes",
		Help: "Host total memory bytes.",
	})
	systemMemUsedBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "system_memory_used_bytes",
		Help: "Host used memory bytes.",
	})

	// disk per mountpoint
	systemDiskTotalBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "system_disk_total_bytes",
		Help: "Total disk bytes for a mountpoint.",
	}, []string{"mountpoint"})

	systemDiskUsedBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "system_disk_used_bytes",
		Help: "Used disk bytes for a mountpoint.",
	}, []string{"mountpoint"})
)

// this function may need to run per server to capture local system metrics

func startSystemCollector(ctx context.Context) {

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {

			select {
			case <-ctx.Done():
				return

			case <-ticker.C:

				// cpu
				pct, err := cpuinfo.Percent(0, false)

				if err == nil && len(pct) > 0 {
					systemCPUPercent.Set(pct[0])
				} else {
					// TODO: log err
				}

				// memory
				m, err := meminfo.VirtualMemory()
				if err == nil {
					systemMemTotalBytes.Set(float64(m.Total))
					systemMemUsedBytes.Set(float64(m.Used))
				} else {
					// TODO: log err
				}

				// disk capture
				parts, err := diskinfo.Partitions(false)
				if err == nil {
					for _, p := range parts {
						if u, err := diskinfo.Usage(p.Mountpoint); err == nil {
							// Use a consistent label key (e.g., mountpoint)
							systemDiskTotalBytes.WithLabelValues(u.Path).Set(float64(u.Total))
							systemDiskUsedBytes.WithLabelValues(u.Path).Set(float64(u.Used))
						} else {
							// TODO: log err
						}
					}
				} else {
					// TODO: log err
				}
			}
		}
	}()
}
