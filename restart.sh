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

# Restart via a full reload (bootout + bootstrap), NOT `kickstart -k`.
# kickstart restarts in-place reusing the cached job context; right after a
# rebuild we've seen it intermittently respawn into a bad state — `alice exec`
# exits 78 (EX_CONFIG) and the service crash-loops even though bob is unlocked
# and a hand-run of the same command works. A clean unload + reload re-reads
# the plist and spawns fresh, which is reliable.
if launchctl print "$DOMAIN/$LABEL" >/dev/null 2>&1; then
  echo "Unloading current instance (bootout)..."
  launchctl bootout "$DOMAIN/$LABEL" 2>/dev/null || true
  # Wait for the job to fully unload before reloading (bootout is async).
  for _ in $(seq 1 20); do
    launchctl print "$DOMAIN/$LABEL" >/dev/null 2>&1 || break
    sleep 0.25
  done
fi
echo "Loading LaunchAgent (bootstrap)..."
launchctl bootstrap "$DOMAIN" "$PLIST"

# Health check. Cold start goes through `alice exec` (bob mTLS handshake + secret
# resolution) before the binary even binds :8080, so give it a generous window —
# a short timeout here false-alarms while the service is still coming up.
echo -n "Waiting for health"
for _ in $(seq 1 30); do
  if curl -sf http://localhost:8080/health >/dev/null 2>&1; then
    echo " OK ✓  → http://localhost:8080/ui"
    exit 0
  fi
  echo -n "."
  sleep 1
done
echo

# Still down after the window — distinguish "launchd gave up" from "slow start".
if launchctl print "$DOMAIN/$LABEL" 2>/dev/null | grep -qE 'state = running'; then
  echo "Health check: process is running but /health didn't answer in 30s — check $LOGFILE."
else
  echo "Health check: not running (check $LOGFILE; ensure 'bob serve' is up for secret injection)."
fi
exit 1
