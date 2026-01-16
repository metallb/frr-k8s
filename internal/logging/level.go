// SPDX-License-Identifier:Apache-2.0

// Package logging sets up structured logging in a uniform way, and
// redirects glog statements into the structured log.
package logging

import (
	"fmt"
	"strings"

	"github.com/go-kit/log/level"
)

const (
	LevelAll   Level = "all"
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
	LevelNone  Level = "none"

	FRRDebugging     LevelFRR = "debugging"
	FRRInformational LevelFRR = "informational"
	FRRWarnings      LevelFRR = "warnings"
	FRRErrors        LevelFRR = "errors"
	FRREmergencies   LevelFRR = "emergencies"
)

var (
	LevelFallback  = LevelInfo
	frrFallback    = FRRInformational
	optionFallback = level.AllowInfo()
)

// Level represents a log level.
type Level string

// NewLevel parses the provided string and returns a pointer to a Level, or an error if the provided
// level string is invalid.
func NewLevel(l string) (Level, error) {
	switch l {
	case string(LevelAll), string(LevelDebug):
		return LevelDebug, nil
	case string(LevelInfo):
		return LevelInfo, nil
	case string(LevelWarn):
		return LevelWarn, nil
	case string(LevelError):
		return LevelError, nil
	case string(LevelNone):
		return LevelNone, nil
	}

	return LevelFallback, fmt.Errorf("invalid log level %q", l)
}

// IsAllOrDebug returns true if the log level is All or Debug.
func (l Level) IsAllOrDebug() bool {
	return l == LevelAll || l == LevelDebug
}

// ToLevelFRR converts the Level to its corresponding FRR daemon log level.
// Returns a pointer to the appropriate LevelFRR constant (FRRDebugging, FRRInformational,
// FRRWarnings, FRRErrors, or FRREmergencies). Falls back to FRRInformational if the level
// is anything unexpected.
func (l Level) ToLevelFRR() LevelFRR {
	switch l {
	case LevelAll, LevelDebug:
		return FRRDebugging
	case LevelInfo:
		return FRRInformational
	case LevelWarn:
		return FRRWarnings
	case LevelError:
		return FRRErrors
	case LevelNone:
		return FRREmergencies
	}

	return frrFallback
}

// ToOption converts the Level to a log level.Option for filtering log output.
// Returns the appropriate level.Option (AllowAll, AllowDebug, AllowInfo, AllowWarn,
// AllowError, or AllowNone) based on the Level value. Falls back to AllowInfo if
// the level is anything unexpected.
func (l Level) ToOption() level.Option {
	switch l {
	case LevelAll:
		return level.AllowAll()
	case LevelDebug:
		return level.AllowDebug()
	case LevelInfo:
		return level.AllowInfo()
	case LevelWarn:
		return level.AllowWarn()
	case LevelError:
		return level.AllowError()
	case LevelNone:
		return level.AllowNone()
	}

	return optionFallback
}

// LevelFRR represents a log level as expected by FRR.
type LevelFRR string

type levelSlice []Level

var (
	// Levels returns an array of valid log levels.
	Levels = levelSlice{LevelAll, LevelDebug, LevelInfo, LevelWarn, LevelError, LevelNone}
)

func (l levelSlice) String() string {
	strs := make([]string, len(l))
	for i, v := range l {
		strs[i] = string(v)
	}
	return strings.Join(strs, ", ")
}
