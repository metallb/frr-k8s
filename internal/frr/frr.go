// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/metallb/frr-k8s/internal/logging"
)

type ConfigHandler interface {
	ApplyConfig(config *Config) error
}

type StatusFetcher interface {
	GetStatus() Status
}

type StatusChanged func()

type Status struct {
	updateTime       string
	Current          string
	LastReloadResult string
}

type FRR struct {
	reloadConfig    chan reloadEvent
	logLevel        string
	Status          Status
	onStatusChanged StatusChanged
	sync.Mutex
}

const ReloadSuccess = "success"

// Create a variable for os.Hostname() in order to make it easy to mock out
// in unit tests.
var osHostname = os.Hostname

func (f *FRR) ApplyConfig(config *Config) error {
	hostname, err := osHostname()
	if err != nil {
		return err
	}

	// TODO add internal wrapper
	config.Loglevel = f.logLevel
	config.Hostname = hostname
	f.reloadConfig <- reloadEvent{config: config}
	return nil
}

var debounceTimeout = 3 * time.Second
var failureTimeout = time.Second * 5

func NewFRR(ctx context.Context, onStatusChanged StatusChanged, logger log.Logger, logLevel logging.Level) *FRR {
	res := &FRR{
		reloadConfig:    make(chan reloadEvent),
		logLevel:        logLevelToFRR(logLevel),
		onStatusChanged: onStatusChanged,
	}
	reload := func(config *Config) error {
		return generateAndReloadConfigFile(config, logger)
	}

	debouncer(ctx, reload, res.reloadConfig, debounceTimeout, failureTimeout, logger)
	res.pollStatus(ctx, logger)
	return res
}

func (f *FRR) GetStatus() Status {
	f.Lock()
	defer f.Unlock()
	return f.Status
}

func (f *FRR) pollStatus(ctx context.Context, l log.Logger) {
	var tickerIntervals = 30 * time.Second
	ticker := time.NewTicker(tickerIntervals)
	go func() {
		for {
			select {
			case <-ticker.C:
				status, err := fetchStatus()
				if err != nil {
					// This doesn't mean the reload failed, but
					// that we were not able to fetch the status
					level.Error(l).Log("op", "fetch status", "error", err)
					break
				}
				if status.updateTime == f.Status.updateTime {
					break
				}
				if status.LastReloadResult != ReloadSuccess {
					level.Error(l).Log("op", "fetch status", "lastReloadResult", "failed")
					f.reloadConfig <- reloadEvent{useOld: true}
				}
				f.Lock()
				f.Status = status
				f.Unlock()
				if f.onStatusChanged != nil {
					f.onStatusChanged()
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

const (
	statusFileName    = "/etc/frr_reloader/.status"
	runningConfig     = "/etc/frr_reloader/running-config"
	lastAppliedResult = "/etc/frr_reloader/last-error"
)

func fetchStatus() (Status, error) {
	timeStamp, status, err := readLastReloadResult()
	if err != nil {
		return Status{}, fmt.Errorf("failed to read status file: %w", err)
	}

	res := Status{
		updateTime: timeStamp,
	}

	bytes, err := os.ReadFile(lastAppliedResult)
	if err != nil && !os.IsNotExist(err) {
		return Status{}, fmt.Errorf("failed to read last error file: %w", err)
	}
	res.LastReloadResult = string(bytes)
	if !strings.Contains(status, "failure") {
		res.LastReloadResult = ReloadSuccess
	}

	bytes, err = os.ReadFile(runningConfig)
	if err != nil && !os.IsNotExist(err) {
		return Status{}, fmt.Errorf("failed to read running config file: %w", err)
	}
	res.Current = string(bytes)

	return res, nil
}

func readLastReloadResult() (string, string, error) {
	bytes, err := os.ReadFile(statusFileName)
	if err != nil {
		return "", "", fmt.Errorf("failed to read status file: %w", err)
	}

	lastReloadStatus := strings.Fields(string(bytes))
	if len(lastReloadStatus) != 2 {
		return "", "", fmt.Errorf("invalid status file format: %s", string(bytes))
	}
	return lastReloadStatus[0], lastReloadStatus[1], nil
}

func logLevelToFRR(level logging.Level) string {
	// Allowed frr log levels are: emergencies, alerts, critical,
	// 		errors, warnings, notifications, informational, or debugging
	switch level {
	case logging.LevelAll, logging.LevelDebug:
		return "debugging"
	case logging.LevelInfo:
		return "informational"
	case logging.LevelWarn:
		return "warnings"
	case logging.LevelError:
		return "error"
	case logging.LevelNone:
		return "emergencies"
	}

	return "informational"
}
