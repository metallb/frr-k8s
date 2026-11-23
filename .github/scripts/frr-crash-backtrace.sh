#!/usr/bin/env bash
# SPDX-License-Identifier:Apache-2.0

set -euo pipefail
umask 077

pid=$1
timestamp=$2
signal=$3
hostname=$4
exe=$(readlink "/proc/$pid/exe") || exit 0
[[ ${exe##*/} == bgpd ]] || exit 0

root="/proc/$pid/root"
report="/var/lib/frr-ci-crashes/bgpd-$pid-$timestamp.log"
core=$(mktemp /var/lib/frr-ci-crashes/core.XXXXXX)
trap 'rm -f "$core"' EXIT

{
  printf 'UTC time %s, hostname %s, host PID %s, signal %s\n' "$(date -u -d "@$timestamp" --iso-8601=seconds)" "$hostname" "$pid" "$signal"
  cat > "$core"
  cat "/proc/$pid/cgroup" "/proc/$pid/status" "/proc/$pid/maps" || true
  readelf --notes "/proc/$pid/exe" || true
  timeout 30s gdb -nx -batch \
    -iex 'set auto-load off' \
    -iex 'set debuginfod enabled off' \
    -ex 'set print frame-arguments none' \
    -ex "set sysroot $root" \
    -ex "set debug-file-directory $root/usr/lib/debug" \
    -ex "file $root$exe" \
    -ex "core-file $core" \
    -ex 'thread apply all bt' \
    -ex 'info registers' \
    -ex 'info sharedlibrary'
} > "$report" 2>&1
