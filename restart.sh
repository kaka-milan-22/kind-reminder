#!/usr/bin/env bash
set -euo pipefail

PIDFILE="kind-reminder.pid"
LOGFILE="kind-reminder.log"
BINARY="./kind-reminder"
BUILD_CMD="go build -o kind-reminder ./cmd/server"

# --- Stop existing process ---
if [ -f "$PIDFILE" ]; then
  PID=$(cat "$PIDFILE")
  if kill -0 "$PID" 2>/dev/null; then
    echo "Stopping kind-reminder (PID $PID)..."
    kill "$PID"
    # Wait up to 5s for graceful shutdown
    for i in $(seq 1 25); do
      if ! kill -0 "$PID" 2>/dev/null; then
        break
      fi
      sleep 0.2
    done
    # Force kill if still alive
    if kill -0 "$PID" 2>/dev/null; then
      echo "Force killing PID $PID..."
      kill -9 "$PID"
    fi
    echo "Stopped."
  else
    echo "PID $PID not running (stale pidfile), removing."
  fi
  rm -f "$PIDFILE"
fi

# --- Build ---
echo "Building..."
$BUILD_CMD
echo "Build OK."

# --- Start ---
echo "Starting kind-reminder..."
$BINARY >> "$LOGFILE" 2>&1 &
echo $! > "$PIDFILE"
echo "Started (PID $(cat $PIDFILE)), logs → $LOGFILE"

# --- Health check ---
sleep 1
if curl -sf http://localhost:8080/health > /dev/null 2>&1; then
  echo "Health check: OK ✓"
else
  echo "Health check: server may still be starting (check $LOGFILE)"
fi
