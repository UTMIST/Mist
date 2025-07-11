package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCanHandleJob(t *testing.T) {
	redisAddr := "localhost:6379"
	consumerID := fmt.Sprintf("worker_%d", os.Getpid())
	supervisor := NewSupervisor(redisAddr, consumerID, "AMD")

	jobAMD := NewJob("", nil, "AMD")
	jobNVIDIA := NewJob("", nil, "NVIDIA")
	jobAny := NewJob("", nil, "")

	assert.True(t, supervisor.canHandleJob(jobAMD))
	assert.True(t, supervisor.canHandleJob(jobAny))
	assert.False(t, supervisor.canHandleJob(jobNVIDIA))
}
