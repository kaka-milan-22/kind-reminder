#!/usr/bin/env bash
set -euo pipefail

# Restore companion to backup_state.sh.
# Downloads a backup (or takes a local .enc), decrypts it via alice/encipherr
# (key from the AnB vault), and extracts the bundle to /tmp for you to review.
# It does NOT overwrite the live service — it prints the put-back commands so you
# stay in control (restoring over a running DB needs care).
#
# Usage:
#   ./restore.sh            # newest backup in gdrive
#   ./restore.sh <NAME.enc> # a specific remote backup name
#   ./restore.sh /path/to/local.enc

export PATH="/Users/bbwave03/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REMOTE_NAME="gdrive"
REMOTE_DIR="kind-reminder-backup"
REMOTE_PATH="${REMOTE_NAME}:${REMOTE_DIR}"
ALICE="/Users/bbwave03/go/bin/alice"
ENCIPHERR_BIN="/Users/bbwave03/.local/bin/encipherr"
ARG="${1:-latest}"

[[ -x "$ALICE" ]]         || { echo "[ERROR] alice not found ($ALICE)"; exit 1; }
[[ -x "$ENCIPHERR_BIN" ]] || { echo "[ERROR] encipherr not found ($ENCIPHERR_BIN)"; exit 1; }
command -v rclone >/dev/null || { echo "[ERROR] rclone not found"; exit 1; }

WORK="$(mktemp -d /tmp/kr-restore-work.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

# ===== Resolve the source .enc =====
if [[ -f "$ARG" ]]; then
  cp "$ARG" "$WORK/"; ENC="${WORK}/$(basename "$ARG")"
  echo "[INFO] Using local file: $ARG"
else
  if [[ "$ARG" == "latest" ]]; then
    NAME=$(rclone lsf "$REMOTE_PATH" --files-only | sort -r | head -1)
    [[ -n "$NAME" ]] || { echo "[ERROR] no backups found in $REMOTE_PATH"; exit 1; }
  else
    NAME="$ARG"
  fi
  echo "[INFO] Downloading $NAME from $REMOTE_PATH"
  rclone copyto "${REMOTE_PATH}/${NAME}" "${WORK}/${NAME}"
  ENC="${WORK}/${NAME}"
fi

# ===== Decrypt =====
echo "[INFO] Decrypt (key from alice/AnB vault)"
TAR="${WORK}/restore.tar.gz"
"$ALICE" exec --env ENCIPHERR_KEY='<agent-vault:encipherr-key>' \
  -- "$ENCIPHERR_BIN" decrypt file "$ENC" -o "$TAR"
[[ -f "$TAR" ]] || { echo "[ERROR] decryption failed"; exit 1; }

# ===== Extract to a persistent /tmp dir for review =====
OUT="/tmp/kr-restored-$(date +%Y%m%d_%H%M%S)"
mkdir -p "$OUT"
tar -xzf "$TAR" -C "$OUT"

echo "[INFO] Restored bundle -> $OUT"
( cd "$OUT" && find . -type f | sed 's|^\./||' )

cat <<EOF

[NEXT] Review the files above. To put them back (stop the service first):

  launchctl bootout gui/\$(id -u)/com.bbwave.kind-reminder
  cp "$OUT/kind-reminder/reminder.db"                    "$SCRIPT_DIR/data/reminder.db"
  cp "$OUT/kind-reminder/config.yaml"                    "$SCRIPT_DIR/config.yaml"
  cp "$OUT/kind-reminder/com.bbwave.kind-reminder.plist" ~/Library/LaunchAgents/
  cp "$OUT/kind-reminder/scripts/"*.sh                   "$SCRIPT_DIR/"
  launchctl bootstrap gui/\$(id -u) ~/Library/LaunchAgents/com.bbwave.kind-reminder.plist

Secrets still come from the vault via alice exec — make sure 'bob' is running.
EOF
