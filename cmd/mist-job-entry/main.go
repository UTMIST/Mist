// Command mist-job-entry is the first process of every Mist job. See
// internal/jobentry and docs/agents/architecture/executors.md.
//
//	mist-job-entry [-spec /etc/mist/job.json]
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/UTMIST/Mist/internal/jobentry"
)

func main() {
	specPath := flag.String("spec", "/etc/mist/job.json", "job description written by the executor")
	flag.Parse()
	// Handle signals before anything else: as PID 1 in a container,
	// unhandled SIGTERM would be ignored.
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)

	spec, err := jobentry.LoadSpec(*specPath)
	if err != nil {
		fmt.Fprintln(os.Stdout, "[mist] invalid job description:", err)
		os.Exit(jobentry.ExitSetupFailed)
	}
	os.Exit(jobentry.Run(spec, os.Stdout, sigs))
}
