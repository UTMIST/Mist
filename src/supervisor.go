package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type Supervisor struct {
	redisClient *redis.Client
	ctx         context.Context
	cancel      context.CancelFunc
	consumerID  string
	gpuType     string
	wg          sync.WaitGroup
}

func NewSupervisor(redisAddr, consumerID, gpuType string) *Supervisor {
	client := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	ctx, cancel := context.WithCancel(context.Background())

	return &Supervisor{
		redisClient: client,
		ctx:         ctx,
		cancel:      cancel,
		consumerID:  consumerID,
		gpuType:     gpuType,
	}
}

func (s *Supervisor) Start() error {
	// Create consumer group if it doesn't exist
	err := s.createConsumerGroup()
	if err != nil {
		return fmt.Errorf("failed to create consumer group: %w", err)
	}

	s.wg.Add(1)
	go s.processJobs()

	log.Printf("Supervisor %s started with GPU: %s", s.consumerID, s.gpuType)
	return nil
}

func (s *Supervisor) createConsumerGroup() error {
	result := s.redisClient.XGroupCreateMkStream(s.ctx, StreamName, ConsumerGroup, "$")
	if result.Err() != nil {
		if result.Err().Error() != "BUSYGROUP Consumer Group name already exists" {
			// in this case the group already exists
			return result.Err()
		}
	}
	return nil
}

func (s *Supervisor) processJobs() {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			// Read from stream with blocking
			result := s.redisClient.XReadGroup(s.ctx, &redis.XReadGroupArgs{
				Group:    ConsumerGroup,
				Consumer: s.consumerID,
				Streams:  []string{StreamName, ">"},
				Count:    1,
				Block:    time.Second * 5,
			})

			if result.Err() != nil {
				if result.Err() != redis.Nil {
					log.Printf("Error reading from stream: %v", result.Err())
				}
				continue
			}

			// Process each message
			for _, stream := range result.Val() {
				for _, message := range stream.Messages {
					s.handleMessage(message)
				}
			}
		}
	}
}

func (s *Supervisor) handleMessage(message redis.XMessage) {
	jobData, ok := message.Values["data"].(string)
	if !ok {
		log.Printf("Invalid job data in message %s", message.ID)
		s.ackMessage(message.ID)
		return
	}

	var job Job
	if err := json.Unmarshal([]byte(jobData), &job); err != nil {
		log.Printf("Failed to unmarshal job data: %v", err)
		s.ackMessage(message.ID)
		return
	}

	// certain jobs require a specific GPU
	if !s.canHandleJob(job) {
		log.Printf("Job %s requires GPU type %s, but supervisor has %s - skipping",
			job.ID, job.RequiredGPU, s.gpuType)
		// let another supervisor can pick it up
		return
	}

	log.Printf("Processing job %s:%s", job.ID, job.Type)

	// Simulate job processing
	success := s.processJob(job)

	if success {
		s.ackMessage(message.ID)
		log.Printf("Job %s completed successfully", job.ID)
	} else {
		log.Printf("Job %s failed", job.ID)
		s.ackMessage(message.ID) // TODO: change this once we have docker support
	}
}

// canHandleJob checks if this supervisor can handle the given job based on GPU requirements
func (s *Supervisor) canHandleJob(job Job) bool {
	// If job doesn't specify GPU requirement, any supervisor can handle it
	if job.RequiredGPU == "" {
		return true
	}

	// Job must match supervisor's GPU type
	return job.RequiredGPU == s.gpuType
}

// TODO: Actually schedule a container here
func (s *Supervisor) processJob(job Job) bool {
	return true
}

func (s *Supervisor) ackMessage(messageID string) {
	result := s.redisClient.XAck(s.ctx, StreamName, ConsumerGroup, messageID)
	if result.Err() != nil {
		log.Printf("Failed to ack message %s: %v", messageID, result.Err())
	}
}

func (s *Supervisor) Stop() {
	log.Printf("Stopping supervisor %s", s.consumerID)
	s.cancel()
	s.wg.Wait()
	s.redisClient.Close()
}
