package multilogger

import (
	"context"
	"errors"
	"fmt"
	"gopkg.in/natefinch/lumberjack.v2"
	"gopkg.in/yaml.v3"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

func (h *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newSubHandlers := make([]slog.Handler, len(h.subHandlers))
	for i, sub := range h.subHandlers {
		newSubHandlers[i] = sub.WithAttrs(attrs)
	}
	return &MultiHandler{subHandlers: newSubHandlers}
}

func (h *MultiHandler) WithGroup(name string) slog.Handler {
	newSubHandlers := make([]slog.Handler, len(h.subHandlers))
	for i, sub := range h.subHandlers {
		newSubHandlers[i] = sub.WithGroup(name)
	}
	return &MultiHandler{subHandlers: newSubHandlers}
}

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

func GetLogConfig() (LogConfig, error) {
	var config LogConfig

	configFile, err := os.ReadFile(LogConfigFilePath)
	if err != nil {
		return config, err
	}

	err = yaml.Unmarshal(configFile, &config)
	if err != nil {
		return config, err
	}

	return config, nil
}

func FallbackLogger(component string) *slog.Logger {
	return slog.New(
		slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}),
	).With("component", component)
}

func CreateLogger(component string, config *LogConfig) (*slog.Logger, error) {
	if err := OverrideYAMLConfig(config); err != nil {
		fallback := FallbackLogger(component)
		fallback.Warn("using fallback logger due to invalid env override")
		return fallback, fmt.Errorf("failed to override YAML config: %w", err)
	}

	writerLevels := make(map[io.Writer]slog.Level)
	for _, t := range config.Output.Types {
		lvl, ok := levelMap[t.Level]
		if !ok {
			fallback := FallbackLogger(component)
			fallback.Warn("using fallback logger due to invalid log level")
			return fallback, fmt.Errorf("invalid log level: %q", t.Level)
		}

		switch t.Type {
		case "stdout":
			writerLevels[os.Stdout] = lvl
		case "file":
			directory := config.Output.Directory

			if err := os.MkdirAll(directory, 0755); err != nil {
				fallback := FallbackLogger(component)
				fallback.Warn("using fallback logger")
				return fallback, fmt.Errorf("failed to create multilogger directory: %w", err)
			}

			filePath := filepath.Join(directory, component+".log")
			rotatingFileWriter := &lumberjack.Logger{
				Filename:   filePath,
				MaxSize:    10,
				MaxBackups: 3,
				MaxAge:     28,
				Compress:   true,
			}

			writerLevels[rotatingFileWriter] = lvl

		default:
			fallback := FallbackLogger(component)
			fallback.Warn("using fallback logger due to unsupported output type", "type", t.Type)
			return fallback, fmt.Errorf("unsupported output type: %q", t.Type)
		}
	}

	if len(writerLevels) == 0 {
		fallback := FallbackLogger(component)
		fallback.Warn("using fallback logger")
		return fallback, errors.New("no valid multilogger outputs configured")
	}

	handler := NewMultiHandler(writerLevels)
	logger := slog.New(handler).With("component", component)
	slog.Info("logger created successfully", "component", component, "outputs", len(writerLevels))
	return logger, nil
}

func OverrideYAMLConfig(config *LogConfig) error {
	if global := os.Getenv("LOG_LEVEL"); global != "" {
		if _, ok := levelMap[global]; ok {
			for i := range config.Output.Types {
				config.Output.Types[i].Level = global
			}
		} else {
			return fmt.Errorf("invalid global LOG_LEVEL: %q", global)
		}
	}

	for i, output := range config.Output.Types {
		env := strings.ToUpper(output.Type) + "_LOG_LEVEL"
		if lvl := strings.TrimSpace(os.Getenv(env)); lvl != "" {
			if _, ok := levelMap[lvl]; ok {
				config.Output.Types[i].Level = lvl
			} else {
				return fmt.Errorf("invalid multilogger level %q for %s", lvl, env)
			}
		}
	}
	return nil
}
