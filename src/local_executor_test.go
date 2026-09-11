package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLocalExecutor(t *testing.T) {
	t.Setenv("MIST_EXECUTOR", "local")
	t.Setenv("MIST_TEST_PROCESS", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"success", "failure", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			output, err := (&Supervisor{}).runGPUCommand(ctx, []string{executable, "-test.run=^TestLocalExecutorHelper$", "--", scenario})
			if (err == nil) != (scenario == "success") {
				t.Fatalf("unexpected exit outcome: %v", err)
			}
			if scenario != "timeout" && !strings.Contains(output, "workload output") {
				t.Fatalf("missing output: %q", output)
			}
			if scenario == "timeout" && ctx.Err() == nil {
				t.Fatal("expected execution deadline")
			}
		})
	}
}

func TestLocalExecutorHelper(t *testing.T) {
	if os.Getenv("MIST_TEST_PROCESS") != "1" {
		return
	}
	scenario := os.Args[len(os.Args)-1]
	if scenario == "timeout" {
		time.Sleep(10 * time.Second)
	}
	fmt.Println("workload output")
	if scenario == "failure" {
		os.Exit(1)
	}
	os.Exit(0)
}
