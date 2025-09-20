package utils

import (
	"io"
	"os"
	"strconv"

	"github.com/sirupsen/logrus"
	kindlog "sigs.k8s.io/kind/pkg/log"
)

// CILogger provides a structured logger that can write to any io.Writer
// It wraps logrus and provides compatibility with kind's logging interfaces
// It implements both kind.Logger and kind.InfoLogger interfaces
type CILogger struct {
	*logrus.Logger
	writer    io.Writer
	verbosity kindlog.Level
}

// NewCILogger creates a new CILogger that writes to the specified writer
// If writer is nil, it defaults to stdout
// verbosity controls kind logging verbosity (0 = only main messages, higher = more verbose)
func NewCILogger(writer io.Writer) *CILogger {
	return NewCILoggerWithVerbosity(writer, KindVerbosityFromEnv())
}

// NewCILoggerWithVerbosity creates a new CILogger with custom verbosity
func NewCILoggerWithVerbosity(writer io.Writer, verbosity kindlog.Level) *CILogger {
	if writer == nil {
		writer = os.Stdout
	}

	logger := logrus.New()
	logger.SetOutput(writer)
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
		ForceColors:   true,
	})
	logger.SetLevel(logrus.InfoLevel)

	return &CILogger{
		Logger:    logger,
		writer:    writer,
		verbosity: verbosity,
	}
}

// Writer returns the underlying io.Writer
func (l *CILogger) Writer() io.Writer {
	return l.writer
}

// Printf provides compatibility with functions expecting a printf-style logger
func (l *CILogger) Printf(format string, args ...interface{}) {
	l.Infof(format, args...)
}

// --- kind.Logger interface implementation ---

// Warn implements kind.Logger interface
func (l *CILogger) Warn(message string) {
	l.Logger.Warn(message)
}

// Warnf implements kind.Logger interface
func (l *CILogger) Warnf(format string, args ...interface{}) {
	l.Logger.Warnf(format, args...)
}

// Error implements kind.Logger interface
func (l *CILogger) Error(message string) {
	l.Logger.Error(message)
}

// Errorf implements kind.Logger interface
func (l *CILogger) Errorf(format string, args ...interface{}) {
	l.Logger.Errorf(format, args...)
}

// V implements kind.Logger interface for verbosity-based logging
func (l *CILogger) V(level kindlog.Level) kindlog.InfoLogger {
	// If the message's verbosity is higher than our configured level,
	// return a no-op logger that discards the message.
	if level > l.verbosity {
		return noopInfoLogger{}
	}
	// Otherwise, return the logger itself to print the message.
	return l
}

// --- kind.InfoLogger interface implementation ---

// Info implements kind.InfoLogger interface
func (l *CILogger) Info(message string) {
	l.Logger.Info(message)
}

// Infof implements kind.InfoLogger interface (already exists from logrus)
// Note: logrus.Logger already has Infof, so we inherit it

// Enabled implements kind.InfoLogger interface
func (l *CILogger) Enabled() bool {
	return true
}

// noopInfoLogger is a logger that does nothing, used to discard messages.
type noopInfoLogger struct{}

func (n noopInfoLogger) Info(message string)                      {}
func (n noopInfoLogger) Infof(format string, args ...interface{}) {}
func (n noopInfoLogger) Enabled() bool                            { return false }

// KindVerbosityFromEnv returns the kind log verbosity level from environment
func KindVerbosityFromEnv() kindlog.Level {
	// Default to 0 (only user-facing messages)
	if vStr := os.Getenv("GROVE_KIND_LOG_VERBOSITY"); vStr != "" {
		if iv, err := strconv.Atoi(vStr); err == nil {
			return kindlog.Level(iv)
		}
	}
	return kindlog.Level(0)
}
