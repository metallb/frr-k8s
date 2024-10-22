// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"syscall"
	"text/template"
	"time"

	"errors"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/metallb/frr-k8s/internal/ipfamily"
)

var (
	configFileName      = "/etc/frr_reloader/frr.conf"
	reloaderPidFileName = "/etc/frr_reloader/reloader.pid"
	//go:embed templates/* templates/*
	templates embed.FS
)

type Config struct {
	Loglevel    string
	Hostname    string
	Routers     []*RouterConfig
	BFDProfiles []BFDProfile
	ExtraConfig string
}

type reloadEvent struct {
	config *Config
	useOld bool
}

type RouterConfig struct {
	MyASN        uint32
	RouterID     string
	Neighbors    []*NeighborConfig
	VRF          string
	IPV4Prefixes []string
	IPV6Prefixes []string
	ImportVRFs   []string
}

type BFDProfile struct {
	Name             string
	ReceiveInterval  *uint32
	TransmitInterval *uint32
	DetectMultiplier *uint32
	EchoInterval     *uint32
	EchoMode         bool
	PassiveMode      bool
	MinimumTTL       *uint32
}

type NeighborConfig struct {
	IPFamily        ipfamily.Family
	Name            string
	ASN             string
	SrcAddr         string
	Addr            string
	Unnumbered      bool
	Port            *uint16
	HoldTime        *int64
	KeepaliveTime   *int64
	ConnectTime     *int64
	Password        string
	BFDProfile      string
	GracefulRestart bool
	EBGPMultiHop    bool
	VRFName         string
	Incoming        AllowedIn
	Outgoing        AllowedOut
	AlwaysBlock     []IncomingFilter
	DisableMP       bool
}

func (n *NeighborConfig) ID() string {
	if n.VRFName == "" {
		return n.Addr
	}
	return fmt.Sprintf("%s-%s", n.Addr, n.VRFName)
}

type AllowedIn struct {
	All        bool
	PrefixesV4 []IncomingFilter
	PrefixesV6 []IncomingFilter
}

func (a *AllowedIn) AllPrefixes() []IncomingFilter {
	return append(a.PrefixesV4, a.PrefixesV6...)
}

type AllowedOut struct {
	PrefixesV4 []OutgoingFilter
	PrefixesV6 []OutgoingFilter
}

func (a *AllowedOut) AllPrefixes() []OutgoingFilter {
	return append(a.PrefixesV4, a.PrefixesV6...)
}

type IncomingFilter struct {
	IPFamily ipfamily.Family
	Prefix   string
	LE       uint32
	GE       uint32
}

func (i IncomingFilter) LessThan(i1 IncomingFilter) bool {
	if i.IPFamily != i1.IPFamily {
		return i.IPFamily < i1.IPFamily
	}
	if i.Prefix != i1.Prefix {
		return i.Prefix < i1.Prefix
	}
	if i.LE != i1.LE {
		return i.LE < i1.LE
	}
	return i.GE < i1.GE
}

func (i IncomingFilter) Matcher() string {
	res := ""
	if i.LE != 0 {
		res += fmt.Sprintf(" le %d", i.LE)
	}
	if i.GE != 0 {
		res += fmt.Sprintf(" ge %d", i.GE)
	}
	return res
}

type OutgoingFilter struct {
	IPFamily         ipfamily.Family
	Prefix           string
	Communities      []string
	LargeCommunities []string
	LocalPref        uint32
}

// templateConfig uses the template library to template
// 'globalConfigTemplate' using 'data'.
func templateConfig(data interface{}) (string, error) {
	counterMap := map[string]int{}
	t, err := template.New("frr.tmpl").Funcs(
		template.FuncMap{
			"counter": func(counterName string) int {
				counter := counterMap[counterName]
				counter++
				counterMap[counterName] = counter
				return counter
			},
			"frrIPFamily": func(ipFamily ipfamily.Family) string {
				if ipFamily == "ipv6" {
					return "ipv6"
				}
				return "ip"
			},
			"activateNeighborFor": func(ipFamily string, neighbourFamily ipfamily.Family, disableMP bool) bool {
				return !disableMP || (disableMP && string(neighbourFamily) == ipFamily)
			},
			"localPrefPrefixList": func(neighbor *NeighborConfig, localPreference uint32) string {
				return fmt.Sprintf("%s-%d-%s-localpref-prefixes", neighbor.ID(), localPreference, neighbor.IPFamily)
			},
			"communityPrefixList": func(neighbor *NeighborConfig, community string) string {
				return fmt.Sprintf("%s-%s-%s-community-prefixes", neighbor.ID(), community, neighbor.IPFamily)
			},
			"largeCommunityPrefixList": func(neighbor *NeighborConfig, community string) string {
				return fmt.Sprintf("%s-large:%s-%s-community-prefixes", neighbor.ID(), community, neighbor.IPFamily)
			},
			"allowedPrefixList": func(neighbor *NeighborConfig) string {
				return fmt.Sprintf("%s-pl-%s", neighbor.ID(), neighbor.IPFamily)
			},
			"allowedIncomingList": func(neighbor *NeighborConfig) string {
				return fmt.Sprintf("%s-inpl-%s", neighbor.ID(), neighbor.IPFamily)
			},
			"deniedIncomingList": func(neighbor *NeighborConfig) string {
				return fmt.Sprintf("%s-denied-inpl-%s", neighbor.ID(), neighbor.IPFamily)
			},
			"mustDisableConnectedCheck": func(ipFamily ipfamily.Family, myASN uint32, asn string, eBGPMultiHop, unnumbered bool) bool {
				// return true only for non-multihop IPv6 eBGP sessions

				if ipFamily != ipfamily.IPv6 {
					return false
				}

				if eBGPMultiHop {
					return false
				}

				if unnumbered {
					return false
				}

				// internal means we expect the session to be iBGP
				if asn == "internal" {
					return false
				}

				// external means we expect the session to be eBGP
				if asn == "external" {
					return true
				}

				// the peer's asn is not dynamic (it is a number),
				// we check if it is different than ours for eBGP
				if strconv.FormatUint(uint64(myASN), 10) != asn {
					return true
				}

				return false
			},
			"dict": func(values ...interface{}) (map[string]interface{}, error) {
				if len(values)%2 != 0 {
					return nil, errors.New("invalid dict call, expecting even number of args")
				}
				dict := make(map[string]interface{}, len(values)/2)
				for i := 0; i < len(values); i += 2 {
					key, ok := values[i].(string)
					if !ok {
						return nil, fmt.Errorf("dict keys must be strings, got %v %T", values[i], values[i])
					}
					dict[key] = values[i+1]
				}
				return dict, nil
			},
		}).ParseFS(templates, "templates/*")
	if err != nil {
		return "", err
	}

	var b bytes.Buffer
	err = t.Execute(&b, data)
	return b.String(), err
}

// writeConfigFile writes the FRR configuration file (represented as a string)
// to 'filename'.
func writeConfig(config string, filename string) error {
	return os.WriteFile(filename, []byte(config), 0600)
}

// reloadConfig requests that FRR reloads the configuration file. This is
// called after updating the configuration.
var reloadConfig = func() error {
	pidFile, found := os.LookupEnv("FRR_RELOADER_PID_FILE")
	if found {
		reloaderPidFileName = pidFile
	}

	pid, err := os.ReadFile(reloaderPidFileName)
	if err != nil {
		return err
	}

	pidInt, err := strconv.Atoi(string(pid))
	if err != nil {
		return err
	}

	// send HUP signal to FRR reloader
	err = syscall.Kill(pidInt, syscall.SIGHUP)
	if err != nil {
		return err
	}

	return nil
}

// generateAndReloadConfigFile takes a 'struct Config' and, using a template,
// generates and writes a valid FRR configuration file. If this completes
// successfully it will also force FRR to reload that configuration file.
func generateAndReloadConfigFile(config *Config, l log.Logger) error {
	filename, found := os.LookupEnv("FRR_CONFIG_FILE")
	if found {
		configFileName = filename
	}

	configString, err := templateConfig(config)
	if err != nil {
		level.Error(l).Log("op", "reload", "error", err, "cause", "template", "config", config)
		return err
	}
	err = writeConfig(configString, configFileName)
	if err != nil {
		level.Error(l).Log("op", "reload", "error", err, "cause", "writeConfig", "config", config)
		return err
	}

	err = reloadConfig()
	if err != nil {
		level.Error(l).Log("op", "reload", "error", err, "cause", "reload", "config", config)
		return err
	}
	return nil
}

// debouncer takes a function that processes an Config, a channel where
// the update requests are sent, and squashes any requests coming in a given timeframe
// as a single request.
func debouncer(ctx context.Context, body func(config *Config) error,
	reload <-chan reloadEvent,
	reloadInterval time.Duration,
	failureRetryInterval time.Duration,
	l log.Logger) {
	go func() {
		var config *Config
		var timeOut <-chan time.Time
		timerSet := false
		for {
			select {
			case newCfg, ok := <-reload:
				if !ok { // the channel was closed
					return
				}
				if newCfg.useOld && config == nil {
					level.Debug(l).Log("op", "reload", "action", "ignore config", "reason", "nil config")
					continue // just ignore the event
				}
				if !newCfg.useOld && reflect.DeepEqual(newCfg.config, config) {
					level.Debug(l).Log("op", "reload", "action", "ignore config", "reason", "same config")
					continue // config hasn't changed
				}
				if !newCfg.useOld {
					config = newCfg.config
				}
				if !timerSet {
					timeOut = time.After(reloadInterval)
					timerSet = true
				}
			case <-timeOut:
				err := body(config)
				if err != nil {
					timeOut = time.After(failureRetryInterval)
					timerSet = true
					continue
				}
				timerSet = false
			case <-ctx.Done():
				return
			}
		}
	}()
}
