// SPDX-License-Identifier:Apache-2.0

// Package logging sets up structured logging in a uniform way, and
// redirects glog statements into the structured log.
package logging

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"k8s.io/klog"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
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

// Init returns a logger configured with common settings like
// timestamping and source code locations. Both the stdlib logger and
// glog are reconfigured to push logs into this logger.
//
// Init must be called as early as possible in main(), before any
// application-specific flag parsing or logging occurs, because it
// mutates the contents of the flag package as well as os.Stderr.
func Init(w io.Writer, lvl Level) (*DynamicLvlLogger, error) {
	l := log.NewJSONLogger(log.NewSyncWriter(w))

	r, w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("creating pipe for glog redirection: %s", err)
	}
	klog.InitFlags(flag.NewFlagSet("klog", flag.ExitOnError))
	klog.SetOutput(w)
	go collectGlogs(r, l)

	timeStampValuer := log.TimestampFormat(time.Now, time.RFC3339)
	l = log.With(l, "ts", timeStampValuer)

	// Note: caller must be added after everything else that decorates the
	// logger (otherwise we get incorrect caller reference).
	l = log.With(l, "caller", log.DefaultCaller)

	// Create our dynamic level logger with the initial log level.
	dl := NewDynamicLvlLogger(l, lvl)

	// Setting a controller-runtime logger is required to
	// get any log created by it.
	ctrl.SetLogger(zap.New())

	return dl, nil
}

// DynamicLvlLogger is a logger with a log level that can be set at runtime.
// We need this new struct because `level.NewFilter" can restrict log levels, but it cannot make them less strict.
// So if we set (pseudo code), in that order:
// 1. LVL = level.NewFilter(LVL, "debug")
// 2. LVL = level.NewFilter(LVL, "error")
// 3. LVL = level.NewFilter(LVL, "info")
// Then the log level would still be 'error', because that's the most restrictive.
// This new struct stores a logger (without the filter applied), and the desired log level.
// The filteredLogger field stores the logger with the filter applied, and is only recreated when
// the log level changes via SetLogLevel.
// The filteredLogger is stored as an atomic pointer to ensure thread-safe access when multiple
// controllers call SetLogLevel concurrently.
type DynamicLvlLogger struct {
	logger         log.Logger
	filteredLogger atomic.Pointer[log.Logger]
}

// NewDynamicLvlLogger creates a new DynamicLvlLogger that wraps the provided logger with
// a dynamically adjustable log level filter. The initial log level is set according to the
// provided level parameter.
func NewDynamicLvlLogger(logger log.Logger, lvl Level) *DynamicLvlLogger {
	filtered := level.NewFilter(logger, lvl.ToOption())
	dl := &DynamicLvlLogger{
		logger: logger,
	}
	dl.filteredLogger.Store(&filtered)
	return dl
}

// Log prints log output.
func (dl *DynamicLvlLogger) Log(keyvals ...interface{}) error {
	return (*dl.filteredLogger.Load()).Log(keyvals...)
}

// SetLogLevel dynamically updates the log level of the DynamicLvlLogger at runtime.
// This allows changing the verbosity of logging without restarting the application.
// Uses atomic operations to ensure thread-safety when called concurrently by multiple controllers.
func (dl *DynamicLvlLogger) SetLogLevel(lvl Level) {
	filtered := level.NewFilter(dl.logger, lvl.ToOption())
	dl.filteredLogger.Store(&filtered)
}

func collectGlogs(f *os.File, logger log.Logger) {
	defer func() {
		if err := f.Close(); err != nil {
			// cant log here, as this is the logger
			errorString := fmt.Sprintf("Error closing file: %s", err)
			panic(errorString)
		}
	}()

	r := bufio.NewReader(f)
	for {
		var buf []byte
		l, pfx, err := r.ReadLine()
		if err != nil {
			// TODO: log
			return
		}
		buf = append(buf, l...)
		for pfx {
			l, pfx, err = r.ReadLine()
			if err != nil {
				// TODO: log
				return
			}
			buf = append(buf, l...)
		}

		leveledLogger, ts, caller, msg := deformat(logger, buf)
		leveledLogger.Log("ts", ts.Format(time.RFC3339Nano), "caller", caller, "msg", msg)
	}
}

var logPrefix = regexp.MustCompile(`^(.)(\d{2})(\d{2}) (\d{2}):(\d{2}):(\d{2}).(\d{6})\s+\d+ ([^:]+:\d+)] (.*)$`)

func deformat(logger log.Logger, b []byte) (leveledLogger log.Logger, ts time.Time, caller, msg string) {
	// Default deconstruction used when anything goes wrong.
	leveledLogger = level.Info(logger)
	ts = time.Now()
	caller = ""
	msg = string(b)

	if len(b) < 30 {
		return
	}

	ms := logPrefix.FindSubmatch(b)
	if ms == nil {
		return
	}

	month, err := strconv.Atoi(string(ms[2]))
	if err != nil {
		return
	}
	day, err := strconv.Atoi(string(ms[3]))
	if err != nil {
		return
	}
	hour, err := strconv.Atoi(string(ms[4]))
	if err != nil {
		return
	}
	minute, err := strconv.Atoi(string(ms[5]))
	if err != nil {
		return
	}
	second, err := strconv.Atoi(string(ms[6]))
	if err != nil {
		return
	}
	micros, err := strconv.Atoi(string(ms[7]))
	if err != nil {
		return
	}
	ts = time.Date(ts.Year(), time.Month(month), day, hour, minute, second, micros*1000, time.Local).UTC()

	switch ms[1][0] {
	case 'I':
		leveledLogger = level.Info(logger)
	case 'W':
		leveledLogger = level.Warn(logger)
	case 'E', 'F':
		leveledLogger = level.Error(logger)
	}

	caller = string(ms[8])
	msg = string(ms[9])

	return
}
