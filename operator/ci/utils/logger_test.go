package utils

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestCILogger(t *testing.T) {
	// Test basic logging functionality
	var buf bytes.Buffer
	logger := NewCILogger(&buf)

	logger.Info("test info message")
	logger.Warn("test warn message")
	logger.Error("test error message")

	output := buf.String()
	if !strings.Contains(output, "test info message") {
		t.Errorf("Expected info message in output, got: %s", output)
	}
	if !strings.Contains(output, "test warn message") {
		t.Errorf("Expected warn message in output, got: %s", output)
	}
	if !strings.Contains(output, "test error message") {
		t.Errorf("Expected error message in output, got: %s", output)
	}
}

func TestCILoggerPrintf(t *testing.T) {
	// Test Printf compatibility
	var buf bytes.Buffer
	logger := NewCILogger(&buf)

	logger.Printf("test printf message with %s", "formatting")

	output := buf.String()
	if !strings.Contains(output, "test printf message with formatting") {
		t.Errorf("Expected formatted message in output, got: %s", output)
	}
}

func TestCILoggerWithNilWriter(t *testing.T) {
	// Test that nil writer defaults to stdout (should not panic)
	logger := NewCILogger(nil)
	if logger == nil {
		t.Error("Expected logger to be created even with nil writer")
	}
	if logger.Writer() == nil {
		t.Error("Expected writer to be set even when passed nil")
	}
}

func TestCILoggerLevels(t *testing.T) {
	// Test different log levels
	var buf bytes.Buffer
	logger := NewCILogger(&buf)

	// Set to debug level to capture all messages
	logger.SetLevel(logrus.DebugLevel)

	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")

	output := buf.String()
	if !strings.Contains(output, "debug message") {
		t.Errorf("Expected debug message in output, got: %s", output)
	}
	if !strings.Contains(output, "info message") {
		t.Errorf("Expected info message in output, got: %s", output)
	}
	if !strings.Contains(output, "warn message") {
		t.Errorf("Expected warn message in output, got: %s", output)
	}
	if !strings.Contains(output, "error message") {
		t.Errorf("Expected error message in output, got: %s", output)
	}
}

func TestKindLoggerCompatibility(t *testing.T) {
	// Test kind logger compatibility using unified CILogger
	var buf bytes.Buffer
	logger := NewCILoggerWithVerbosity(&buf, KindVerbosityFromEnv())

	// Test kind.Logger interface methods
	logger.Warn("kind warn message")
	logger.Error("kind error message")

	// Test kind.InfoLogger interface methods
	logger.Info("kind info message")
	logger.Infof("kind formatted message: %s", "test")

	// Test verbosity filtering
	infoLogger := logger.V(0) // Should be enabled
	if !infoLogger.Enabled() {
		t.Error("Expected V(0) logger to be enabled")
	}
	infoLogger.Info("verbose info message")

	output := buf.String()
	if !strings.Contains(output, "kind warn message") {
		t.Errorf("Expected kind warn message in output, got: %s", output)
	}
	if !strings.Contains(output, "kind error message") {
		t.Errorf("Expected kind error message in output, got: %s", output)
	}
	if !strings.Contains(output, "kind info message") {
		t.Errorf("Expected kind info message in output, got: %s", output)
	}
	if !strings.Contains(output, "kind formatted message: test") {
		t.Errorf("Expected formatted message in output, got: %s", output)
	}
}
