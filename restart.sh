#!/usr/bin/env bash
set -euo pipefail

# kind-reminder is managed by launchd (see deploy/com.bbwave.kind-reminder.plist,
# installed at ~/Library/LaunchAgents/). This script rebuilds the binary and asks
# launchd to restart the service — it does NOT start the process by hand, which
# would race the managed instance on :8080. Secrets are injected by the plist via
# `alice exec`, so the AnB `bob` daemon must be running + unlocked.

LABEL="com.bbwave.kind-reminder"
DOMAIN="gui/$(id -u)"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
LOGFILE="kind-reminder.log"

cd "$(dirname "$0")"

echo "Building..."
go build -o kind-reminder ./cmd/server
echo "Build OK."

if launchctl print "$DOMAIN/$LABEL" >/dev/null 2>&1; then
  echo "Restarting service (launchctl kickstart)..."
  launchctl kickstart -k "$DOMAIN/$LABEL"
else
  echo "Loading LaunchAgent..."
  launchctl bootstrap "$DOMAIN" "$PLIST"
fi

# Health check
sleep 1.5
for _ in $(seq 1 10); do
  if curl -sf http://localhost:8080/health >/dev/null 2>&1; then
    echo "Health check: OK ✓  → http://localhost:8080/ui"
    exit 0
  fi
  sleep 0.5
done
echo "Health check: not responding (check $LOGFILE; ensure 'bob serve' is up for secret injection)"
exit 1
