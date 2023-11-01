// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestHealthCheck(t *testing.T) {
	counter := 0
	tests := []struct {
		name            string
		hc              healthChecker
		expectedCounter int
		shouldErr       bool
	}{
		{
			hc: healthChecker{
				attempts: 5,
				check: func() error {
					counter++
					return nil
				},
			},
			expectedCounter: 1,
			shouldErr:       false,
		},
		{
			hc: healthChecker{
				attempts: 5,
				check: func() error {
					counter++
					return errors.New("err")
				},
			},
			expectedCounter: 5,
			shouldErr:       true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			counter = 0
			err := test.hc.Run()
			if test.shouldErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !test.shouldErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if diff := cmp.Diff(counter, test.expectedCounter); diff != "" {
				t.Fatalf("config different from expected: %s", diff)
			}
		})
	}
}
