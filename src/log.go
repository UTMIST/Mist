package main

import (
	"context"
	"gopkg.in/yaml.v3"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

const LogConfigFilePath = "../config/log.yaml"

type MultiHandler struct {
	subHandlers []slog.Handler
	level       slog.Level
}

type LogConfig struct {
	Output struct {
		Types     []string `yaml:"types"`
		Directory string   `yaml:"directory"`
	} `yaml:"output"`
	Components []string `yaml:"components"`
}

func NewMultiHandler(level slog.Level, writers []io.Writer) *MultiHandler {
	var handlers []slog.Handler

	for _, writer := range writers {
		handlers = append(handlers, slog.NewJSONHandler(writer, nil))
	}

	m := MultiHandler{
		level:       level,
		subHandlers: handlers,
	}
	return &m
}

func (h *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool { return h.level <= level }
func (h *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler           { return h }
func (h *MultiHandler) WithGroup(name string) slog.Handler                 { return h }
func (h *MultiHandler) Handle(ctx context.Context, record slog.Record) error {
	var err error
	for _, handler := range h.subHandlers {
		if out := handler.Handle(ctx, record); out != nil {
			err = out
		}
	}
	return err
}

func getLogConfig(file string) (LogConfig, error) {
	var config LogConfig

	configFile, err := os.ReadFile(file)
	if err != nil {
		return config, err
	}

	err = yaml.Unmarshal(configFile, &config)
	if err != nil {
		return config, err
	}

	return config, nil
}

func createLogger(component string) (*slog.Logger, error) {
	logConfig, err := getLogConfig(LogConfigFilePath)
	if err != nil {
		return nil, err
	}
	var writers []io.Writer
	for _, t := range logConfig.Output.Types {
		switch t {
		case "stdout":
			writers = append(writers, os.Stdout)
		case "file":
			directory := logConfig.Output.Directory

			if err := os.MkdirAll(directory, 0755); err != nil {
				return nil, err
			}

			filePath := filepath.Join(directory, component+".log")
			file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err != nil {
				return nil, err
			}
			writers = append(writers, file)

		}
	}
	handler := NewMultiHandler(slog.LevelInfo, writers)
	logger := slog.New(handler).With("component", component)
	return logger, nil
}
