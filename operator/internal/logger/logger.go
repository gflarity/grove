// /*
// Copyright 2024 The Grove Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// */

// Package logger provides utilities for creating and configuring structured loggers
// using the logr interface backed by Zap logging framework.
package logger

import (
	"fmt"

	configv1alpha1 "github.com/NVIDIA/grove/operator/api/config/v1alpha1"

	"github.com/go-logr/logr"
	"go.uber.org/zap/zapcore"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	logzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// MustNewLogger creates a new logr.Logger backed by Zap and panics on invalid input.
// The devMode parameter enables development-friendly logging output.
// The level parameter sets the minimum log level (debug, info, error).
// The format parameter controls output format (json or text).
func MustNewLogger(devMode bool, level configv1alpha1.LogLevel, format configv1alpha1.LogFormat) logr.Logger {
	// Build logger options from configuration parameters
	opts, err := buildDefaultLoggerOpts(devMode, level, format)
	utilruntime.Must(err)
	return logzap.New(opts...)
}

// buildDefaultLoggerOpts constructs Zap logger options from configuration parameters.
// Returns a slice of logzap.Opts that configure development mode, log format, and log level.
func buildDefaultLoggerOpts(devMode bool, level configv1alpha1.LogLevel, format configv1alpha1.LogFormat) ([]logzap.Opts, error) {
	var opts []logzap.Opts

	// Configure development mode for human-readable output
	opts = append(opts, logzap.UseDevMode(devMode))

	// Configure log format (JSON or text)
	formatOpts, err := createLogFormatOpts(format)
	if err != nil {
		return nil, err
	}
	opts = append(opts, formatOpts)

	// Configure log level filtering
	levelOpts, err := createLogLevelOpts(level)
	if err != nil {
		return nil, err
	}
	opts = append(opts, levelOpts)

	return opts, nil
}

// createLogLevelOpts converts a Grove log level to Zap logger options.
// Supports debug, info, and error levels with info as the default for empty values.
func createLogLevelOpts(level configv1alpha1.LogLevel) (logzap.Opts, error) {
	var zapLevel zapcore.LevelEnabler

	// Map Grove log levels to Zap levels
	switch level {
	case configv1alpha1.DebugLevel:
		zapLevel = zapcore.DebugLevel
	case configv1alpha1.ErrorLevel:
		zapLevel = zapcore.ErrorLevel
	case "", configv1alpha1.InfoLevel:
		zapLevel = zapcore.InfoLevel
	default:
		return nil, fmt.Errorf("invalid log level %q", level)
	}

	return logzap.Level(zapLevel), nil
}

// createLogFormatOpts converts a Grove log format to Zap logger options.
// Supports text (console) and JSON formats with JSON as the default for empty values.
func createLogFormatOpts(format configv1alpha1.LogFormat) (logzap.Opts, error) {
	// Configure common encoder settings for consistent timestamp and duration formatting
	setCommonEncoderConfigOpts := func(encoderConfig *zapcore.EncoderConfig) {
		encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
		encoderConfig.EncodeDuration = zapcore.StringDurationEncoder
	}

	// Map Grove log formats to Zap encoders
	switch format {
	case configv1alpha1.LogFormatText:
		return logzap.ConsoleEncoder(setCommonEncoderConfigOpts), nil
	case "", configv1alpha1.LogFormatJSON:
		return logzap.JSONEncoder(setCommonEncoderConfigOpts), nil
	default:
		return nil, fmt.Errorf("invalid log format %q", format)
	}
}
