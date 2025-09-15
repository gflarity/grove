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

// Package opts provides command-line options and configuration management for the Grove operator.
// It handles parsing configuration files, validating operator settings, and integrating with
// the Kubernetes configuration scheme.
package opts

import (
	"fmt"
	"os"

	configv1alpha1 "github.com/NVIDIA/grove/operator/api/config/v1alpha1"
	operatorvalidation "github.com/NVIDIA/grove/operator/api/config/validation"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

// configDecoder is a runtime decoder for parsing operator configuration files.
var configDecoder runtime.Decoder

// init initializes the configuration decoder by setting up the runtime scheme
// and creating a universal decoder for operator configuration objects.
func init() {
	configScheme := runtime.NewScheme()
	utilruntime.Must(configv1alpha1.AddToScheme(configScheme))
	configDecoder = serializer.NewCodecFactory(configScheme).UniversalDecoder()
}

// CLIOptions provides a convenient abstraction for initializing and validating
// OperatorConfiguration from command-line flags and configuration files.
type CLIOptions struct {
	// configFile is the path to the operator configuration file.
	configFile string
	// Config is the parsed operator configuration loaded from the configuration file.
	Config *configv1alpha1.OperatorConfiguration
}

// NewCLIOptions creates a new CLIOptions instance and registers the required
// command-line flags with the provided flag set.
func NewCLIOptions(fs *pflag.FlagSet) *CLIOptions {
	cliOpts := &CLIOptions{}
	cliOpts.addFlags(fs)
	return cliOpts
}

// Complete reads the configuration file specified by the --config flag and
// decodes it into an OperatorConfiguration object. This method must be called
// after the command-line flags have been parsed.
func (o *CLIOptions) Complete() error {
	if len(o.configFile) == 0 {
		return fmt.Errorf("missing config file")
	}
	data, err := os.ReadFile(o.configFile)
	if err != nil {
		return fmt.Errorf("error reading config file: %w", err)
	}
	o.Config = &configv1alpha1.OperatorConfiguration{}
	if err = runtime.DecodeInto(configDecoder, data, o.Config); err != nil {
		return fmt.Errorf("error decoding config: %w", err)
	}
	return nil
}

// Validate performs validation on the loaded OperatorConfiguration using the
// operator's validation rules. This method should be called after Complete().
func (o *CLIOptions) Validate() error {
	if errs := operatorvalidation.ValidateOperatorConfiguration(o.Config); errs != nil {
		return errs.ToAggregate()
	}
	return nil
}

// addFlags registers the command-line flags with the provided flag set.
func (o *CLIOptions) addFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.configFile, "config", o.configFile, "Path to configuration file.")
}
