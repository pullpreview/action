package pullpreview

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

type Logger struct {
	level LogLevel
	base  *log.Logger
}

func NewLogger(level LogLevel) *Logger {
	return &Logger{
		level: level,
		base:  log.New(os.Stdout, "", 0),
	}
}

func ParseLogLevel(value string) LogLevel {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "DEBUG":
		return LevelDebug
	case "INFO":
		return LevelInfo
	case "WARN", "WARNING":
		return LevelWarn
	case "ERROR":
		return LevelError
	default:
		return LevelInfo
	}
}

func (l *Logger) SetLevel(level LogLevel) {
	l.level = level
}

func (l *Logger) Debugf(format string, args ...any) {
	if l.level <= LevelDebug {
		l.printf("DEBUG", format, args...)
	}
}

func (l *Logger) Infof(format string, args ...any) {
	if l.level <= LevelInfo {
		l.printf("INFO", format, args...)
	}
}

func (l *Logger) Warnf(format string, args ...any) {
	if l.level <= LevelWarn {
		l.printf("WARN", format, args...)
	}
}

func (l *Logger) Errorf(format string, args ...any) {
	if l.level <= LevelError {
		l.printf("ERROR", format, args...)
	}
}

func (l *Logger) printf(prefix string, format string, args ...any) {
	timestamp := time.Now().Format(time.RFC3339)
	l.base.Printf("%s %s %s", timestamp, prefix, fmt.Sprintf(format, args...))
}
