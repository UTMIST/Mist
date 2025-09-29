package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	cpuinfo "github.com/shirou/gopsutil/v3/cpu"
	diskinfo "github.com/shirou/gopsutil/v3/disk"
	meminfo "github.com/shirou/gopsutil/v3/mem"
)

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
	runningJobs = promauto.NewGaugeVec(prometheus.GaugeOpts{
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

type Metrics struct {
	HTTPInFlight        prometheus.Gauge
	HTTPRequestsTotal   *prometheus.CounterVec
	HTTPRequestDuration *prometheus.HistogramVec

	JobsStarted   *prometheus.CounterVec
	JobsCompleted *prometheus.CounterVec
	JobsFailed    *prometheus.CounterVec
	RunningJobs   *prometheus.GaugeVec

	SystemCPUPercent     prometheus.Gauge
	SystemMemTotalBytes  prometheus.Gauge
	SystemMemUsedBytes   prometheus.Gauge
	SystemDiskTotalBytes *prometheus.GaugeVec
	SystemDiskUsedBytes  *prometheus.GaugeVec
}

func NewMetrics() *Metrics {
	return &Metrics{
		HTTPInFlight:        httpInFlight,
		HTTPRequestsTotal:   httpRequestsTotal,
		HTTPRequestDuration: httpRequestDuration,

		JobsStarted:   jobsStarted,
		JobsCompleted: jobsCompleted,
		JobsFailed:    jobsFailed,
		RunningJobs:   runningJobs,

		SystemCPUPercent:     systemCPUPercent,
		SystemMemTotalBytes:  systemMemTotalBytes,
		SystemMemUsedBytes:   systemMemUsedBytes,
		SystemDiskTotalBytes: systemDiskTotalBytes,
		SystemDiskUsedBytes:  systemDiskUsedBytes,
	}
}

func (m *Metrics) StartCollecting(ctx context.Context) {
	startSystemCollector(ctx)

}

// Wrap http wraps around the http handlers to collect metrics
func (m *Metrics) WrapHTTP(name string, next http.Handler) http.Handler {
	return promhttp.InstrumentHandlerInFlight(
		httpInFlight,
		promhttp.InstrumentHandlerDuration(
			httpRequestDuration.MustCurryWith(prometheus.Labels{"handler": name}),
			promhttp.InstrumentHandlerCounter(
				httpRequestsTotal.MustCurryWith(prometheus.Labels{"handler": name}),
				next,
			),
		),
	)
}

func (m *Metrics) TrackJob(ctx context.Context, jobType, gpu string, fn func(context.Context) error) error {

	jobsStarted.WithLabelValues(jobType, gpu).Inc()
	runningJobs.WithLabelValues(gpu).Inc()
	defer runningJobs.WithLabelValues(gpu).Dec()

	err := fn(ctx)

	if err != nil {
		jobsFailed.WithLabelValues(jobType, gpu).Inc()
		return err
	}
	jobsCompleted.WithLabelValues(jobType, gpu).Inc()
	return nil
}

// Collects metrics from the host where this process is running, these values reflect the local machine
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
					slog.Error("failed to get cpu percent", "err", err)
				}

				// memory
				m, err := meminfo.VirtualMemory()
				if err == nil {
					systemMemTotalBytes.Set(float64(m.Total))
					systemMemUsedBytes.Set(float64(m.Used))
				} else {
					slog.Error("failed to get memory info", "err", err)
				}

				// disk capture
				parts, err := diskinfo.Partitions(false)
				if err == nil {
					for _, p := range parts {
						if u, err := diskinfo.Usage(p.Mountpoint); err == nil {

							systemDiskTotalBytes.WithLabelValues(u.Path).Set(float64(u.Total))
							systemDiskUsedBytes.WithLabelValues(u.Path).Set(float64(u.Used))
						} else {
							slog.Error("failed to get disk usage", "mountpoint", p.Mountpoint)
						}
					}
				} else {
					slog.Error("failed to get disk partitions")
				}
			}
		}
	}()
}
