# multilogger

`multilogger` lets you log to multiple io.Writer instances at once, with JSON structured logs, file rotation, and flexible metadata.

---

## Overview


Multilogger allows you to write logs to multiple io.Writer targets (e.g. stdout, files, etc.) simultaneously, each with its own log level.
It’s ideal for tracking runtime activity across components such as schedulers, supervisors, and the main application.
If the configuration fails or a destination cannot be initialized, multilogger automatically falls back to a stderr logger.
File rotation is handled using [lumberjack](https://github.com/natefinch/lumberjack). Environment variables are able to override the log levels configured in the log.yaml file.

---

## Example Usage
```go
// Create Multilogger
config := multilogger.GetLogConfig()
logger, _ := multilogger.CreateLogger("app", &config)
logger.Info("starting app")
```

```go
// Set Global Log Level
os.Setenv("LOG_LEVEL", "DEBUG")
```

```go
// Configure Log Levels By io.Writer
os.Setenv("FILE_LOG_LEVEL", "DEBUG")
os.Setenv("STDOUT_LOG_LEVEL", "INFO")
```

```go
// Add Metadata
config := multilogger.GetLogConfig()
logger, _ := multilogger.CreateLogger("app", &config)
logger = logger.WithGroup("jobInfo")
logger.Info("job started", "job_id", "abc123")
```

---
