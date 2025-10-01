package main

import (
	"context"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

const LogConfigFilePath = "../config/log.yaml"

var levelMap = map[string]slog.Level{
	"DEBUG": slog.LevelDebug,
	"INFO":  slog.LevelInfo,
	"WARN":  slog.LevelWarn,
	"ERROR": slog.LevelError,
}

type MultiHandler struct {
	subHandlers []slog.Handler
}

type OutputType struct {
	Type  string `yaml:"type"`
	Level string `yaml:"level"`
}

type LogConfig struct {
	Output struct {
		Types     []OutputType `yaml:"types"`
		Directory string       `yaml:"directory"`
	} `yaml:"output"`
	Components []string `yaml:"components"`
}

func NewMultiHandler(writerLevels map[io.Writer]slog.Level) *MultiHandler {
	var handlers []slog.Handler

	for writer, level := range writerLevels {
		handlers = append(handlers, slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level}))
	}

	m := MultiHandler{
		subHandlers: handlers,
	}
	return &m
}

func (h *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.subHandlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false

}

func (h *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *MultiHandler) WithGroup(name string) slog.Handler       { return h }
func (h *MultiHandler) Handle(ctx context.Context, record slog.Record) error {
	var err error
	for _, handler := range h.subHandlers {
		if handler.Enabled(ctx, record.Level) {
			if out := handler.Handle(ctx, record); out != nil {
				err = out
			}
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

func fallbackLogger(component string) *slog.Logger {
	return slog.New(
		slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}),
	).With("component", component)
}

func createLogger(component string) (*slog.Logger, error) {
	logConfig, err := getLogConfig(LogConfigFilePath)
	if err != nil {
		fallback := fallbackLogger(component)
		fallback.Warn("using fallback logger")
		return fallback, fmt.Errorf("failed to load log config: %w", err)
	}

	writerLevels := make(map[io.Writer]slog.Level)
	for _, t := range logConfig.Output.Types {
		switch t.Type {
		case "stdout":
			writerLevels[os.Stdout] = levelMap[t.Level]
		case "file":
			directory := logConfig.Output.Directory

			if err := os.MkdirAll(directory, 0755); err != nil {
				fallback := fallbackLogger(component)
				fallback.Warn("using fallback logger")
				return fallback, fmt.Errorf("failed to create log directory: %w", err)
			}

			filePath := filepath.Join(directory, component+".log")
			file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err != nil {
				fallback := fallbackLogger(component)
				fallback.Warn("using fallback logger")
				return fallback, fmt.Errorf("failed to open log file %q: %w", filePath, err)
			}
			writerLevels[file] = levelMap[t.Level]
		}
	}

	if len(writerLevels) == 0 {
		fallback := fallbackLogger(component)
		fallback.Warn("using fallback logger")
		return fallback, errors.New("no valid log outputs configured")
	}

	handler := NewMultiHandler(writerLevels)
	logger := slog.New(handler).With("component", component)
	return logger, nil
}
