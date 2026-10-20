#!/usr/bin/env bash
# Every 5 min, if free space on the data volume drops under 30 GB, clear the Go build cache and the
# golangci-lint cache, then post an agent-comms bus note. Rebuild cost only; deletes nothing else.
set -euo pipefail
THRESHOLD_GB=30
BUS=$HOME/.claude/agent-comms/messages
while true; do
  free_gb=$(df -g /System/Volumes/Data | awk 'NR==2 {print $4}')
  if [ -n "$free_gb" ] && [ "$free_gb" -lt "$THRESHOLD_GB" ]; then
    ts=$(date -u +%Y%m%dT%H%M%SZ)
    go clean -cache || true
    GOTOOLCHAIN=go1.26.9 go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.10.1 cache clean >/dev/null 2>&1 || true
    after_gb=$(df -g /System/Volumes/Data | awk 'NR==2 {print $4}')
    echo "result $ts disk-guard: free space was ${free_gb} GB (< ${THRESHOLD_GB} GB), so I cleared the Go build and golangci-lint caches; free now ${after_gb} GB. Re-run any build or test that failed with IO, cache or space errors." > "$BUS/result-$ts-disk-guard.txt"
    echo "$ts cleared: ${free_gb} GB -> ${after_gb} GB"
  fi
  sleep 300
done
