package utils

import (
	"io"
	"os"
	"strconv"

	"github.com/sirupsen/logrus"
	kindlog "sigs.k8s.io/kind/pkg/log"
)

// CiLogger provides a structured logger that can write to any io.Writer
// It wraps logrus and provides compatibility with kind's logging interfaces
// It implements both kind.Logger and kind.InfoLogger interfaces
type CiLogger struct {
	*logrus.Logger
	writer    io.Writer
	closer    func()
	verbosity kindlog.Level
}

// NewCiLogger creates a new CiLogger that writes to the specified writer
// If writer is nil, it defaults to stdout
// verbosity controls kind logging verbosity (0 = only main messages, higher = more verbose)
func NewCiLogger(writer io.Writer) *CiLogger {
	return NewCiLoggerWithVerbosity(writer, KindVerbosityFromEnv())
}

// NewCiLoggerWithVerbosity creates a new CiLogger with custom verbosity
func NewCiLoggerWithVerbosity(writer io.Writer, verbosity kindlog.Level) *CiLogger {
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

	return &CiLogger{
		Logger:    logger,
		writer:    writer,
		closer:    func() {}, // no-op by default
		verbosity: verbosity,
	}
}

// NewCiLoggerWithFile creates a CiLogger that tees output to stdout and a file
// This is used to log the output of the k8s clusters setup process to a file for realtime tailing
// as it takes a while to setup the clusters and we want to be able to see the progress for debugging purposes.
// The file path is controlled by GROVE_CI_LOG_PATH (default: /tmp/grove-ci.log).
// It returns the logger and a close function to release resources when done.
func NewCiLoggerWithFile() (*CiLogger, func()) {
	path := os.Getenv("GROVE_CI_LOG_PATH")
	if path == "" {
		path = "/tmp/grove-ci.log"
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		// Fallback: just stdout; closer is no-op
		logger := NewCiLogger(os.Stdout)
		return logger, func() {}
	}

	mw := io.MultiWriter(os.Stdout, f)
	logger := NewCiLogger(mw)

	closer := func() {
		_ = f.Close()
	}
	logger.closer = closer

	return logger, closer
}

// Writer returns the underlying io.Writer
func (l *CiLogger) Writer() io.Writer {
	return l.writer
}

// Close calls the closer function if set
func (l *CiLogger) Close() {
	l.closer()
}

// Printf provides compatibility with functions expecting a printf-style logger
func (l *CiLogger) Printf(format string, args ...interface{}) {
	l.Infof(format, args...)
}

// --- kind.Logger interface implementation ---

// Warn implements kind.Logger interface
func (l *CiLogger) Warn(message string) {
	l.Logger.Warn(message)
}

// Warnf implements kind.Logger interface
func (l *CiLogger) Warnf(format string, args ...interface{}) {
	l.Logger.Warnf(format, args...)
}

// Error implements kind.Logger interface
func (l *CiLogger) Error(message string) {
	l.Logger.Error(message)
}

// Errorf implements kind.Logger interface
func (l *CiLogger) Errorf(format string, args ...interface{}) {
	l.Logger.Errorf(format, args...)
}

// V implements kind.Logger interface for verbosity-based logging
func (l *CiLogger) V(level kindlog.Level) kindlog.InfoLogger {
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
func (l *CiLogger) Info(message string) {
	l.Logger.Info(message)
}

// Infof implements kind.InfoLogger interface (already exists from logrus)
// Note: logrus.Logger already has Infof, so we inherit it

// Enabled implements kind.InfoLogger interface
func (l *CiLogger) Enabled() bool {
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
