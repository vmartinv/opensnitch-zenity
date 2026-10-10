package log

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

type Logger struct {
	level Level
}

var globalLogger = &Logger{level: LevelInfo}

// SetLevel sets the global log level
func SetLevel(levelStr string) {
	level := parseLevel(levelStr)
	globalLogger.level = level
}

// Debug logs a debug message
func Debug(msg string, args ...interface{}) {
	globalLogger.log(LevelDebug, msg, args...)
}

// Info logs an info message
func Info(msg string, args ...interface{}) {
	globalLogger.log(LevelInfo, msg, args...)
}

// Warn logs a warning message
func Warn(msg string, args ...interface{}) {
	globalLogger.log(LevelWarn, msg, args...)
}

// Error logs an error message
func Error(msg string, args ...interface{}) {
	globalLogger.log(LevelError, msg, args...)
}

func (l *Logger) log(level Level, msg string, args ...interface{}) {
	if level < l.level {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	levelStr := levelToString(level)
	message := fmt.Sprintf(msg, args...)
	fmt.Fprintf(os.Stderr, "[%s] %s: %s\n", timestamp, levelStr, message)
}

func parseLevel(levelStr string) Level {
	switch strings.ToLower(levelStr) {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

func levelToString(level Level) string {
	switch level {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}
