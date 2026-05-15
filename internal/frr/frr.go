// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"net"
	"os"
	"strings"
	"sync"
	"time"

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
	config.Hostname = hostname
	// On IPv6-only nodes, FRR defaults router-id to 0.0.0.0 (RFC 6286 violation).
	if !hasIPv4Address() {
		routerID := hashRouterID(hostname)
		for _, r := range config.Routers {
			if r.RouterID == "" {
				r.RouterID = routerID
			}
		}
	}
	f.reloadConfig <- reloadEvent{config: config}
	return nil
}

var netInterfaces = net.Interfaces

func hasIPv4Address() bool {
	ifaces, err := netInterfaces()
	if err != nil {
		return false
	}
	for _, i := range ifaces {
		addrs, err := i.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				return true
			}
		}
	}
	return false
}

func hashRouterID(hostname string) string {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, crc32.ChecksumIEEE([]byte(hostname)))
	return net.IP(b).String()
}

var failureTimeout = time.Second * 5

func NewFRR(ctx context.Context, onStatusChanged StatusChanged, debounceTimeout time.Duration) *FRR {
	res := &FRR{
		reloadConfig:    make(chan reloadEvent),
		onStatusChanged: onStatusChanged,
	}

	debouncer(ctx, generateAndReloadConfigFile, res.reloadConfig, debounceTimeout, failureTimeout)
	res.pollStatus(ctx)
	return res
}

func (f *FRR) GetStatus() Status {
	f.Lock()
	defer f.Unlock()
	return f.Status
}

func (f *FRR) pollStatus(ctx context.Context) {
	l := logging.GetLogger()
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
