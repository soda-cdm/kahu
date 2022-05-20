package options

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

func (options *LoggingOptions) AddLoggingFlags(fs *pflag.FlagSet) {
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
