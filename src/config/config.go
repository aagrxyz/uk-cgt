package config

import (
	"os"

	log "github.com/sirupsen/logrus"
	prefixed "github.com/x-cray/logrus-prefixed-formatter"
)

const logTimeFormatString = "Mon Jan _2 15:04:05 2006"

type ExecutionMode int

const (
	INTERACTIVE_MODE ExecutionMode = iota
	SERVER_MODE
)

var mode = INTERACTIVE_MODE

func Mode() ExecutionMode {
	return mode
}

func SetMode(m ExecutionMode) {
	mode = m
}

func init() {
	formatter := new(prefixed.TextFormatter)
	formatter.DisableColors = true
	formatter.FullTimestamp = true
	formatter.ForceFormatting = true
	formatter.TimestampFormat = logTimeFormatString
	log.SetFormatter(formatter)
	// Output to stdout instead of the default stderr
	log.SetOutput(os.Stdout)
}

func Env(key, fallback string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return fallback
	}
	return value
}
