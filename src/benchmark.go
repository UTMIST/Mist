package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

//go:embed workloads/cuda_benchmark.py
var benchmarkScript string

func cudaImage() string {
	return envOr("MIST_CUDA_IMAGE", "pytorch/pytorch:2.5.1-cuda12.4-cudnn9-runtime")
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func benchmarkSize(payload map[string]interface{}) (int, error) {
	value, exists := payload["matrix_size"]
	if !exists {
		return 2048, nil
	}
	size, ok := value.(float64)
	if !ok || (size != 1024 && size != 2048 && size != 4096) {
		return 0, fmt.Errorf("matrix_size must be 1024, 2048, or 4096")
	}
	return int(size), nil
}

func (s *Supervisor) processBenchmark(job Job) bool {
	var output string
	var result map[string]interface{}
	size, err := benchmarkSize(job.Payload)
	if err == nil && (job.RequiredGPU != "NVIDIA" || s.gpuType != "NVIDIA") {
		err = fmt.Errorf("CUDA benchmark requires an NVIDIA worker")
	}
	if err == nil {
		ctx, cancel := context.WithTimeout(s.ctx, 90*time.Second)
		defer cancel()
		output, err = s.runGPUCommand(ctx, []string{"python", "-u", "-c", benchmarkScript, strconv.Itoa(size)})
	}
	if err == nil {
		for _, line := range strings.Split(output, "\n") {
			if strings.HasPrefix(line, "MIST_RESULT=") {
				err = json.Unmarshal([]byte(strings.TrimPrefix(line, "MIST_RESULT=")), &result)
			}
		}
		if err == nil && (result == nil || result["verified"] != true) {
			err = fmt.Errorf("workload did not produce a verified result")
		}
	}
	fields := map[string]interface{}{"logs": output, "completed": time.Now().Format(time.RFC3339Nano)}
	if err != nil {
		fields["error"] = err.Error()
	}
	if result != nil {
		encoded, _ := json.Marshal(result)
		fields["result"] = string(encoded)
	}
	// Preserve failure details even when shutdown cancelled the workload context.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if saveErr := s.redisClient.HSet(ctx, "job:"+job.ID, fields).Err(); saveErr != nil {
		s.log.Error("cannot save benchmark result", "error", saveErr)
		return false
	}
	return err == nil
}
