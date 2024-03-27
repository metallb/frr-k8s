# syntax=docker/dockerfile:1.2

FROM --platform=$BUILDPLATFORM docker.io/golang:1.21.8 AS builder
ARG GIT_COMMIT=dev
ARG GIT_BRANCH=dev
WORKDIR $GOPATH/frr-k8s

# Cache the downloads
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/main.go cmd/main.go
COPY api/ api/
COPY internal/ internal/
COPY frr-tools/metrics ./frr-tools/metrics/

ARG TARGETARCH
ARG TARGETOS
ARG TARGETPLATFORM

# have to manually convert as building the different arms can cause issues
# Extract variant
RUN case ${TARGETPLATFORM} in \
  "linux/arm/v6") export VARIANT="6" ;; \
  "linux/arm/v7") export VARIANT="7" ;; \
  *) export VARIANT="" ;; \
  esac

# Cache builds directory for faster rebuild
RUN --mount=type=cache,target=/root/.cache/go-build \
  --mount=type=cache,target=/go/pkg \
  # build frr metrics
  CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=$VARIANT \
  go build -v -o /build/frr-metrics \
  -ldflags "-X 'frr-k8s/internal/version.gitCommit=${GIT_COMMIT}' -X 'frr-k8s/metallb/internal/version.gitBranch=${GIT_BRANCH}'" \
  frr-tools/metrics/exporter.go \
  && \
  CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=$VARIANT \
  go build -v -o /build/frr-k8s \
  -ldflags "-X 'frr-k8s/internal/version.gitCommit=${GIT_COMMIT}' -X 'frr-k8s/internal/version.gitBranch=${GIT_BRANCH}'" \
  cmd/main.go

FROM docker.io/alpine:latest


COPY --from=builder /build/frr-k8s /frr-k8s
COPY --from=builder /build/frr-metrics /frr-metrics
COPY frr-tools/reloader/frr-reloader.sh /frr-reloader.sh
COPY LICENSE /

LABEL org.opencontainers.image.authors="metallb" \
  org.opencontainers.image.url="https://github.com/metallb/frr-k8s" \
  org.opencontainers.image.source="https://github.com/metallb/frr-k8s" \
  org.opencontainers.image.vendor="metallb" \
  org.opencontainers.image.licenses="Apache-2.0" \
  org.opencontainers.image.description="FRR-K8s" \
  org.opencontainers.image.title="frr-k8s" \
  org.opencontainers.image.base.name="docker.io/alpine:latest"

ENTRYPOINT ["/frr-k8s"]
