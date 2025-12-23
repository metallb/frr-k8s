// SPDX-License-Identifier:Apache-2.0

package logging

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/go-kit/log/level"
)

func TestSetLogLevel(t *testing.T) {
	tcs := []struct {
		from    string
		to      string
		expects []string
	}{
		{
			from:    "error",
			to:      "debug",
			expects: []string{"two"},
		},
		{
			from:    "debug",
			to:      "error",
			expects: []string{"one"},
		},
		{
			from:    "debug",
			to:      "info",
			expects: []string{"one", "two"},
		},
	}
	for _, tc := range tcs {
		t.Run(fmt.Sprintf("from %s to %s", tc.from, tc.to), func(t *testing.T) {
			var buf bytes.Buffer
			logger, err := Init(&buf, tc.from)
			if err != nil {
				t.Error(err)
			}
			level.Info(logger).Log("controller", "one")
			if err = logger.SetLogLevel(tc.to); err != nil {
				t.Error(err)
			}
			level.Info(logger).Log("controller", "two")
			for _, elem := range tc.expects {
				if !strings.Contains(buf.String(), elem) {
					t.Errorf("expected log output to contain element %q, log output: %q", elem, buf.String())
				}
			}
		})
	}
}
