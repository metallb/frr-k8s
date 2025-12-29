// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"github.com/metallb/frr-k8s/internal/logging"
)

const (
	LevelDebugging     Level = "debugging"
	LevelInformational Level = "informational"
	LevelWarnings      Level = "warnings"
	LevelErrors        Level = "errors"
	LevelEmergencies   Level = "emergencies"
)

var (
	levelFallback = LevelInformational
)

// Level represents a log level as expected by FRR.
type Level string

// LevelFrom converts the logging.Level to its corresponding FRR daemon log level.
// Falls back to LevelInformational if the level is anything unexpected.
func LevelFrom(l logging.Level) Level {
	switch l {
	case logging.LevelAll, logging.LevelDebug:
		return LevelDebugging
	case logging.LevelInfo:
		return LevelInformational
	case logging.LevelWarn:
		return LevelWarnings
	case logging.LevelError:
		return LevelErrors
	case logging.LevelNone:
		return LevelEmergencies
	}

	return levelFallback
}
