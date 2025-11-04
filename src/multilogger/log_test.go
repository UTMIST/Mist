package multilogger

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func getLogMessages(text *bytes.Buffer) []string {
	scanner := bufio.NewScanner(text)
	msgs := []string{}

	for scanner.Scan() {
		obj := map[string]interface{}{}
		line := scanner.Text()
		json.Unmarshal([]byte(line), &obj)
		msgs = append(msgs, obj["msg"].(string))
	}
	return msgs
}

func TestMultiHandler(t *testing.T) {
	t.Run("test multiple writers with different log levels", func(t *testing.T) {
		writer1 := &bytes.Buffer{}
		writer2 := &bytes.Buffer{}
		writer3 := &bytes.Buffer{}

		writerLevels := map[io.Writer]slog.Level{
			writer1: slog.LevelInfo,
			writer2: slog.LevelDebug,
			writer3: slog.LevelError,
		}

		handler := NewMultiHandler(writerLevels)
		logger := slog.New(handler)

		logger.Debug("debug")
		logger.Info("info")
		logger.Warn("warn")
		logger.Error("error")

		expectedWriter1 := []string{"info", "warn", "error"}
		expectedWriter2 := []string{"debug", "info", "warn", "error"}
		expectedWriter3 := []string{"error"}

		if !reflect.DeepEqual(getLogMessages(writer1), expectedWriter1) {
			t.Errorf("writer1: got %v, want %v", getLogMessages(writer1), expectedWriter1)
		}
		if !reflect.DeepEqual(getLogMessages(writer2), expectedWriter2) {
			t.Errorf("writer2: got %v, want %v", getLogMessages(writer2), expectedWriter2)
		}
		if !reflect.DeepEqual(getLogMessages(writer3), expectedWriter3) {
			t.Errorf("writer3: got %v, want %v", getLogMessages(writer3), expectedWriter3)
		}
	})

	t.Run("test that WithGroup works with multiple handlers ", func(t *testing.T) {
		writer1 := &bytes.Buffer{}
		writer2 := &bytes.Buffer{}

		writerLevels := map[io.Writer]slog.Level{
			writer1: slog.LevelInfo,
			writer2: slog.LevelDebug,
		}

		handler := NewMultiHandler(writerLevels)
		logger := slog.New(handler)

		groupLogger := logger.WithGroup("request")
		groupLogger.Info("group test", "id", 123)

		obj1 := map[string]interface{}{}
		obj2 := map[string]interface{}{}
		json.Unmarshal(writer1.Bytes(), &obj1)
		json.Unmarshal(writer2.Bytes(), &obj2)

		requestGroup1, ok := obj1["request"].(map[string]interface{})
		if !ok {
			t.Fatal("expected 'request' to be a map")
		}

		requestGroup2, ok := obj2["request"].(map[string]interface{})
		if !ok {
			t.Fatal("expected 'request' to be a map")
		}

		if requestGroup1["id"] != float64(123) {
			t.Errorf("expected id 123, got %v", requestGroup1["id"])
		}

		if requestGroup2["id"] != float64(123) {
			t.Errorf("expected id 123, got %v", requestGroup2["id"])
		}

	})

	t.Run("test that WithGroup works where a sub handler with a higher level should not recieve a group", func(t *testing.T) {
		writer1 := &bytes.Buffer{}
		writer2 := &bytes.Buffer{}

		writerLevels := map[io.Writer]slog.Level{
			writer1: slog.LevelInfo,
			writer2: slog.LevelError,
		}

		handler := NewMultiHandler(writerLevels)
		logger := slog.New(handler)

		groupLogger := logger.WithGroup("request")
		groupLogger.Info("group test", "id", 123)

		obj1 := map[string]interface{}{}
		obj2 := map[string]interface{}{}
		json.Unmarshal(writer1.Bytes(), &obj1)
		json.Unmarshal(writer2.Bytes(), &obj2)

		requestGroup1, ok := obj1["request"].(map[string]interface{})
		if !ok {
			t.Fatal("expected 'request' to be a map")
		}

		if _, ok := obj2["request"]; ok {
			t.Fatal("did not expect 'request' group to exist for this writer")
		}

		if requestGroup1["id"] != float64(123) {
			t.Errorf("expected id 123, got %v", requestGroup1["id"])
		}

	})

	t.Run("test WithAttrs with multiple sub handlers", func(t *testing.T) {
		writer1 := &bytes.Buffer{}
		writer2 := &bytes.Buffer{}

		writerLevels := map[io.Writer]slog.Level{
			writer1: slog.LevelInfo,
			writer2: slog.LevelDebug,
		}

		handler := NewMultiHandler(writerLevels)
		newHandler := handler.WithAttrs([]slog.Attr{
			slog.String("exampleKey1", "testName"),
			slog.Int("exampleKey2", 123),
		})

		logger := slog.New(newHandler)
		logger.Info("info message")

		obj1 := map[string]interface{}{}
		obj2 := map[string]interface{}{}
		json.Unmarshal(writer1.Bytes(), &obj1)
		json.Unmarshal(writer2.Bytes(), &obj2)

		if obj1["exampleKey1"] != "testName" {
			t.Fatal("expected 'exampleKey1' to be 'testName'")
		}

		if int(obj1["exampleKey2"].(float64)) != 123 {
			t.Fatal("expected 'exampleKey2' to be '123'")
		}

		if obj2["exampleKey1"] != "testName" {
			t.Fatal("expected 'exampleKey1' to be 'testName'")
		}

		if int(obj2["exampleKey2"].(float64)) != 123 {
			t.Fatal("expected 'exampleKey2' to be '123'")
		}

	})
}

func TestOverrideYAMLConfig(t *testing.T) {
	t.Run("test with no env variables", func(t *testing.T) {
		os.Clearenv()
		config := &LogConfig{
			Output: struct {
				Types     []OutputType `yaml:"types"`
				Directory string       `yaml:"directory"`
			}{
				Directory: t.TempDir(),
				Types: []OutputType{
					{Type: "stdout", Level: "INFO"},
					{Type: "file", Level: "DEBUG"},
				},
			},
			Components: []string{"app"},
		}

		err := OverrideYAMLConfig(config)
		if err != nil {
			t.Fatal(err)
		}

		if config.Output.Types[0].Level != "INFO" {
			t.Fatal("expected 'stdout' to be 'INFO'")
		}

		if config.Output.Types[1].Level != "DEBUG" {
			t.Fatal("expected 'file' to be 'DEBUG'")
		}

	})
	t.Run("test global env variable", func(t *testing.T) {
		os.Clearenv()
		config := &LogConfig{
			Output: struct {
				Types     []OutputType `yaml:"types"`
				Directory string       `yaml:"directory"`
			}{
				Directory: t.TempDir(),
				Types: []OutputType{
					{Type: "stdout", Level: "WARN"},
					{Type: "file", Level: "DEBUG"},
				},
			},
			Components: []string{"app"},
		}

		os.Setenv("LOG_LEVEL", "INFO")

		err := OverrideYAMLConfig(config)
		if err != nil {
			t.Fatal(err)
		}

		if config.Output.Types[0].Level != "INFO" {
			t.Fatal("expected 'stdout' to be 'INFO'")
		}

		if config.Output.Types[1].Level != "INFO" {
			t.Fatal("expected 'file' to be 'INFO'")
		}

	})

	t.Run("test env variables for different handlers", func(t *testing.T) {
		os.Clearenv()
		config := &LogConfig{
			Output: struct {
				Types     []OutputType `yaml:"types"`
				Directory string       `yaml:"directory"`
			}{
				Directory: t.TempDir(),
				Types: []OutputType{
					{Type: "stdout", Level: "WARN"},
					{Type: "file", Level: "DEBUG"},
				},
			},
			Components: []string{"app"},
		}

		os.Setenv("STDOUT_LOG_LEVEL", "DEBUG")
		os.Setenv("FILE_LOG_LEVEL", "INFO")

		err := OverrideYAMLConfig(config)
		if err != nil {
			t.Fatal(err)
		}

		if config.Output.Types[0].Level != "DEBUG" {
			t.Fatal("expected 'stdout' to be 'DEBUG'")
		}

		if config.Output.Types[1].Level != "INFO" {
			t.Fatal("expected 'file' to be 'INFO'")
		}

	})

}

func TestFallbackLogger(t *testing.T) {
	t.Run("invalid log level triggers fallback", func(t *testing.T) {
		os.Clearenv()
		config := &LogConfig{
			Output: struct {
				Types     []OutputType `yaml:"types"`
				Directory string       `yaml:"directory"`
			}{
				Directory: t.TempDir(),
				Types:     []OutputType{{Type: "stdout", Level: "INVALID"}},
			},
			Components: []string{"app"},
		}

		logger, err := CreateLogger("app", config)
		if err == nil {
			t.Fatal("expected error due to invalid log level")
		}
		if logger == nil {
			t.Fatal("expected fallback logger to be returned")
		}

	})

	t.Run("uncreatable directory triggers fallback", func(t *testing.T) {
		os.Clearenv()
		config := &LogConfig{
			Output: struct {
				Types     []OutputType `yaml:"types"`
				Directory string       `yaml:"directory"`
			}{
				Directory: "/root/invaliddir",
				Types:     []OutputType{{Type: "file", Level: "INFO"}},
			},
			Components: []string{"app"},
		}

		logger, err := CreateLogger("app", config)
		if err == nil {
			t.Fatal("expected error due to directory creation failure")
		}
		if logger == nil {
			t.Fatal("expected fallback logger to be returned")
		}

	})

	t.Run("no outputs configured triggers fallback", func(t *testing.T) {
		os.Clearenv()
		config := &LogConfig{
			Output: struct {
				Types     []OutputType `yaml:"types"`
				Directory string       `yaml:"directory"`
			}{
				Directory: t.TempDir(),
				Types:     []OutputType{},
			},
			Components: []string{"app"},
		}

		logger, err := CreateLogger("app", config)
		if err == nil {
			t.Fatal("expected error due to no outputs configured")
		}
		if logger == nil {
			t.Fatal("expected fallback logger to be returned")
		}

	})

	t.Run("unsupported output type triggers fallback", func(t *testing.T) {
		os.Clearenv()
		config := &LogConfig{
			Output: struct {
				Types     []OutputType `yaml:"types"`
				Directory string       `yaml:"directory"`
			}{
				Directory: t.TempDir(),
				Types:     []OutputType{{Type: "network", Level: "INFO"}},
			},
			Components: []string{"app"},
		}

		logger, err := CreateLogger("app", config)
		if err == nil {
			t.Fatal("expected error due to unsupported output type")
		}
		if logger == nil {
			t.Fatal("expected fallback logger to be returned")
		}
	})

	t.Run("missing directory for file output triggers fallback", func(t *testing.T) {
		os.Clearenv()
		config := &LogConfig{
			Output: struct {
				Types     []OutputType `yaml:"types"`
				Directory string       `yaml:"directory"`
			}{
				Directory: "",
				Types:     []OutputType{{Type: "file", Level: "INFO"}},
			},
			Components: []string{"app"},
		}

		logger, err := CreateLogger("app", config)
		if err == nil {
			t.Fatal("expected error due to missing directory")
		}
		if logger == nil {
			t.Fatal("expected fallback logger to be returned")
		}
	})

}

func TestCreateLogger(t *testing.T) {
	t.Run("valid config creates multi-handler successfully", func(t *testing.T) {
		os.Clearenv()
		tmpDir := t.TempDir()
		config := &LogConfig{
			Output: struct {
				Types     []OutputType `yaml:"types"`
				Directory string       `yaml:"directory"`
			}{
				Directory: tmpDir,
				Types: []OutputType{
					{Type: "stdout", Level: "INFO"},
					{Type: "file", Level: "DEBUG"},
				},
			},
			Components: []string{"app"},
		}

		logger, err := CreateLogger("app", config)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if logger == nil {
			t.Fatal("expected logger to be created")
		}

		testMsg := "this should go into the file"
		logger.Info(testMsg)

		logFilePath := filepath.Join(tmpDir, "app.log")
		time.Sleep(50 * time.Millisecond)

		if _, err := os.Stat(logFilePath); err != nil {
			if os.IsNotExist(err) {
				t.Fatalf("expected log file to exist at %s", logFilePath)
			}
			t.Fatalf("failed to stat log file: %v", err)
		}

		content, err := os.ReadFile(logFilePath)
		if err != nil {
			t.Fatalf("failed to read log file: %v", err)
		}
		if !strings.Contains(string(content), testMsg) {
			t.Fatalf("expected log file to contain %q, got:\n%s", testMsg, string(content))
		}

	})
}
