package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/redis/go-redis/v9"
)

type Supervisor struct {
	client         *redis.Client
	dockerClient   *client.Client
	ctx            context.Context
	cancel         context.CancelFunc
	consumerID     string
	wg             sync.WaitGroup
	maxContainers  int
	containerMutex sync.RWMutex
}

func NewSupervisor(redisAddr, consumerID string) (*Supervisor, error) {
	redisClient := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Supervisor{
		client:        redisClient,
		dockerClient:  dockerClient,
		ctx:           ctx,
		cancel:        cancel,
		consumerID:    consumerID,
		maxContainers: 4,
	}, nil
}

func (s *Supervisor) Start() error {
	// Create consumer group if it doesn't exist
	err := s.createConsumerGroup()
	if err != nil {
		return fmt.Errorf("failed to create consumer group: %w", err)
	}

	s.wg.Add(1)
	go s.processJobs()

	log.Printf("Supervisor %s started with max containers: %d", s.consumerID, s.maxContainers)
	return nil
}

func (s *Supervisor) createConsumerGroup() error {
	result := s.client.XGroupCreateMkStream(s.ctx, StreamName, ConsumerGroup, "$")
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
			if !s.canStartNewContainer() {
				log.Printf("Container limit reached (%d), not claiming job", s.maxContainers)
				time.Sleep(5 * time.Second)
				continue
			}

			// Read from stream with blocking
			result := s.client.XReadGroup(s.ctx, &redis.XReadGroupArgs{
				Group:    ConsumerGroup,
				Consumer: s.consumerID,
				Streams:  []string{StreamName, ">"},
				Count:    1,
				Block:    time.Second * 3,
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

	log.Printf("Processing job %s of type %s", job.ID, job.Type)

	// Simulate job processing
	success := s.processJob(job)
	if success {
		s.ackMessage(message.ID)
		log.Printf("Job %s completed successfully", job.ID)
	} else {
		log.Printf("Job %s failed", job.ID) // maybe don't do this?
		s.ackMessage(message.ID)            // TODO: change this once we have docker support
	}
}

func (s *Supervisor) canStartNewContainer() bool {
	s.containerMutex.RLock()
	defer s.containerMutex.RUnlock()

	containers, err := s.dockerClient.ContainerList(s.ctx, container.ListOptions{})
	if err != nil {
		log.Printf("Error getting container count: %v", err)
		return false
	}

	runningCount := len(containers)

	log.Printf("Current running containers: %d, Max: %d", runningCount, s.maxContainers)
	return runningCount < s.maxContainers
}

// TODO: Actually schedule a container here
func (s *Supervisor) processJob(job Job) bool {
	return true
}

func (s *Supervisor) ackMessage(messageID string) {
	result := s.client.XAck(s.ctx, StreamName, ConsumerGroup, messageID)
	if result.Err() != nil {
		log.Printf("Failed to ack message %s: %v", messageID, result.Err())
	}
}

func (s *Supervisor) Stop() {
	log.Printf("Stopping supervisor %s", s.consumerID)
	s.cancel()
	s.wg.Wait()
	s.client.Close()

	if s.dockerClient != nil {
		s.dockerClient.Close()
	}
}
