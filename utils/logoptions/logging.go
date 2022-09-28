/*
Copyright 2022 The SODA Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
  http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package options defines log options
package logoptions

import (
	"fmt"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
)

const (
	defaultLogFormat = "text"
	defaultLogLevel  = log.InfoLevel
)

// LogOptions maintains log initialization flags
type LogOptions struct {
	// Logging format option
	logFormat string
	// Log level option
	logLevel string
}

// NewLogOptions initialize logging with default options and return logging flag object
func NewLogOptions() *LogOptions {
	loggingOptions := &LogOptions{
		logFormat: defaultLogFormat,
		logLevel:  defaultLogLevel.String(),
	}
	err := loggingOptions.Apply()
	if err != nil {
		log.Error("fail to apply log options")
		return nil
	}
	return loggingOptions
}

// AddFlags adds logging flags in framework
func (options *LogOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&options.logLevel, "logLevel", options.logLevel,
		"Set logging level. options(debug, info, warning, fatal)")
	fs.StringVar(&options.logFormat, "logFormat", options.logFormat,
		"Set logging format. option(text)")
}

// Apply initializes logging module with logging options
func (options *LogOptions) Apply() error {
	err := options.setLoggingLevel()
	if err != nil {
		return err
	}

	err = options.setLoggingFormat()
	if err != nil {
		return err
	}

	return nil
}

func (options *LogOptions) setLoggingLevel() error {
	var loggingLevel log.Level

	switch options.logLevel {
	case "info":
		loggingLevel = log.InfoLevel
	case "debug":
		loggingLevel = log.DebugLevel
	case "warning":
		loggingLevel = log.WarnLevel
	case "fatal":
		loggingLevel = log.FatalLevel
	default:
		return fmt.Errorf("invalid logging level [%s]", options.logLevel)
	}

	log.SetLevel(loggingLevel)
	return nil
}

func (options *LogOptions) setLoggingFormat() error {
	var formatter log.Formatter
	switch options.logFormat {
	case "text":
		formatter = &log.TextFormatter{
			FullTimestamp: true,
		}
	default:
		return fmt.Errorf("invalid error format %s", options.logFormat)
	}

	log.SetFormatter(formatter)
	return nil
}
