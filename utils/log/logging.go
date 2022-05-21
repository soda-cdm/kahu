// Copyright 2022 The SODA Authors.
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

package log

import (
	"fmt"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
)

type (
	LogFormat string
	LogLevel  string
)

type LoggingOptions struct {
	LogFormat string
	LogLevel  string
}

func NewLoggingOptions() *LoggingOptions {
	loggingOptions := &LoggingOptions{
		LogFormat: "text",
		LogLevel:  log.InfoLevel.String(),
	}
	_ = loggingOptions.Apply()
	return loggingOptions
}

func (options *LoggingOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&options.LogLevel, "loglevel", "info",
		"Set logging level. options(debug, info, warning, fatal)")
	fs.StringVar(&options.LogFormat, "logformat", "text",
		"Set logging format. option(text)")
}

func (options *LoggingOptions) Apply() error {
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

func (options *LoggingOptions) setLoggingLevel() error {
	var loggingLevel log.Level

	switch options.LogLevel {
	case "info":
		loggingLevel = log.InfoLevel
	case "debug":
		loggingLevel = log.DebugLevel
	case "warning":
		loggingLevel = log.WarnLevel
	case "fatal":
		loggingLevel = log.FatalLevel
	default:
		return fmt.Errorf("invalid logging level [%s]", options.LogLevel)
	}

	log.SetLevel(loggingLevel)
	return nil
}

func (options *LoggingOptions) setLoggingFormat() error {
	var formatter log.Formatter
	switch options.LogFormat {
	case "text":
		formatter = &log.TextFormatter{
			FullTimestamp: true,
		}
	default:
		return fmt.Errorf("invalid error format %s", options.LogFormat)
	}

	log.SetFormatter(formatter)
	return nil
}
