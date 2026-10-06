#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WEB_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TMP_DIR="$WEB_ROOT/.tmp"
RESTART_FILE="$TMP_DIR/playwright-live-backend.restart"
SUPERVISOR_PID_FILE="$TMP_DIR/playwright-live-backend-supervisor.pid"
CHILD_PID_FILE="$TMP_DIR/playwright-live-backend-child.pid"

mkdir -p "$TMP_DIR"
rm -f "$RESTART_FILE"
echo $$ > "$SUPERVISOR_PID_FILE"

export AUTOCARTO_HOME="${AUTOCARTO_HOME:-${HOME}/.autocarto}"

CHILD_PID=""

start_child() {
  lsof -ti:3000 | xargs -r kill >/dev/null 2>&1 || true
  (
    cd "$WEB_ROOT/../carto_agent"
    exec go run ./cmd/agent
  ) &
  CHILD_PID=$!
  echo "$CHILD_PID" > "$CHILD_PID_FILE"
}

stop_child() {
  if [[ -n "$CHILD_PID" ]] && kill -0 "$CHILD_PID" >/dev/null 2>&1; then
    kill "$CHILD_PID" >/dev/null 2>&1 || true
    wait "$CHILD_PID" 2>/dev/null || true
  fi
  CHILD_PID=""
  rm -f "$CHILD_PID_FILE"
}

cleanup() {
  stop_child
  rm -f "$RESTART_FILE" "$SUPERVISOR_PID_FILE"
}

trap cleanup EXIT INT TERM

start_child

while true; do
  if [[ -f "$RESTART_FILE" ]]; then
    rm -f "$RESTART_FILE"
    stop_child
    start_child
  elif [[ -n "$CHILD_PID" ]] && ! kill -0 "$CHILD_PID" >/dev/null 2>&1; then
    start_child
  fi
  sleep 1
done
