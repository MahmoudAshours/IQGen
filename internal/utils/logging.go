package utils

import (
	"log"
	"os"
	"strings"
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

func NewLogger(level string) *Logger {
	lvl := LevelInfo
	switch strings.ToLower(level) {
	case "debug":
		lvl = LevelDebug
	case "info":
		lvl = LevelInfo
	case "warn", "warning":
		lvl = LevelWarn
	case "error":
		lvl = LevelError
	}
	return &Logger{
		level: lvl,
		base:  log.New(os.Stdout, "", log.LstdFlags),
	}
}

func (l *Logger) Debugf(format string, args ...any) {
	if l.level <= LevelDebug {
		l.base.Printf("DEBUG: "+format, args...)
	}
}

func (l *Logger) Infof(format string, args ...any) {
	if l.level <= LevelInfo {
		l.base.Printf("INFO: "+format, args...)
	}
}

func (l *Logger) Warnf(format string, args ...any) {
	if l.level <= LevelWarn {
		l.base.Printf("WARN: "+format, args...)
	}
}

func (l *Logger) Errorf(format string, args ...any) {
	if l.level <= LevelError {
		l.base.Printf("ERROR: "+format, args...)
	}
}
