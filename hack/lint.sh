#!/bin/bash
set -o errexit
set -x

GOLANGCI_LINT_VERSION="${GOLANGCI_LINT_VERSION:-1.57.1}"
CMD="golangci-lint run --timeout 10m0s ./..."
ENV="${ENV:-container}"

if [ "$ENV" == "container" ]; then
     docker run --rm -v $(git rev-parse --show-toplevel):/app -w /app golangci/golangci-lint:v$GOLANGCI_LINT_VERSION $CMD
else
     curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v"$GOLANGCI_LINT_VERSION"
     $CMD
fi
