#!/usr/bin/env bash
set -euo pipefail

# Manual, complete-restore backup of kind-reminder:
#   - data/reminder.db   (SQLite state: jobs, steps, executions)  -- atomic .backup
#   - config.yaml        (config; secrets are blanked / live in the vault)
#   - *.sh               (restart.sh, this script, any other scripts)
#   - com.bbwave.kind-reminder.plist  (the launchd service definition)
# All bundled into one tar, encrypted with encipherr (key injected from the AnB
# vault via alice — never on disk), then uploaded to Google Drive.
#
# Run it by hand whenever you want a snapshot. No cron / no scheduled job.

export PATH="/Users/bbwave03/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ===== Config =====
DB_FILE="${SCRIPT_DIR}/data/reminder.db"
CONFIG_FILE="${SCRIPT_DIR}/config.yaml"
PLIST="${HOME}/Library/LaunchAgents/com.bbwave.kind-reminder.plist"

REMOTE_NAME="gdrive"
REMOTE_DIR="kind-reminder-backup"
REMOTE_PATH="${REMOTE_NAME}:${REMOTE_DIR}"
KEEP=10

ALICE="/Users/bbwave03/go/bin/alice"
ENCIPHERR_BIN="/Users/bbwave03/.local/bin/encipherr"

DATE_STR=$(date +"%Y%m%d_%H%M%S")

# ===== Checks =====
[[ -f "$DB_FILE" ]]        || { echo "[ERROR] reminder.db not found: $DB_FILE"; exit 1; }
[[ -x "$ALICE" ]]         || { echo "[ERROR] alice not found ($ALICE)"; exit 1; }
[[ -x "$ENCIPHERR_BIN" ]] || { echo "[ERROR] encipherr not found ($ENCIPHERR_BIN)"; exit 1; }
command -v rclone  >/dev/null || { echo "[ERROR] rclone not found"; exit 1; }
command -v sqlite3 >/dev/null || { echo "[ERROR] sqlite3 not found"; exit 1; }

# ===== Stage everything in /tmp (repo dir stays clean) =====
WORK="$(mktemp -d /tmp/kr-backup.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT
PAYLOAD="${WORK}/kind-reminder"
mkdir -p "${PAYLOAD}/scripts"

echo "[INFO] Step1: SQLite atomic backup"
sqlite3 "$DB_FILE" ".backup '${PAYLOAD}/reminder.db'"
[[ -f "${PAYLOAD}/reminder.db" ]] || { echo "[ERROR] sqlite backup failed"; exit 1; }

echo "[INFO] Step2: Collect config / scripts / plist"
[[ -f "$CONFIG_FILE" ]] && cp "$CONFIG_FILE" "${PAYLOAD}/config.yaml" || echo "[WARN] config.yaml missing, skipped"
cp "${SCRIPT_DIR}"/*.sh "${PAYLOAD}/scripts/" 2>/dev/null || true
[[ -f "$PLIST" ]] && cp "$PLIST" "${PAYLOAD}/" || echo "[WARN] plist missing, skipped"
echo "[INFO]   bundled: $(cd "$PAYLOAD" && find . -type f | sed 's|^\./||' | tr '\n' ' ')"

echo "[INFO] Step3: Archive"
ARCHIVE="${WORK}/kind-reminder_${DATE_STR}.tar.gz"
tar -czf "$ARCHIVE" -C "$WORK" kind-reminder

echo "[INFO] Step4: Encrypt (key from alice/AnB vault)"
"$ALICE" exec --env ENCIPHERR_KEY='<agent-vault:encipherr-key>' \
  -- "$ENCIPHERR_BIN" encrypt file "$ARCHIVE"
ENC_FILE="${ARCHIVE}.enc"
BASENAME="$(basename "$ENC_FILE")"
[[ -f "$ENC_FILE" ]] || { echo "[ERROR] encryption failed"; exit 1; }

echo "[INFO] Step5: Upload to ${REMOTE_PATH}/${BASENAME}"
rclone mkdir "$REMOTE_PATH" || true
rclone copyto "$ENC_FILE" "${REMOTE_PATH}/${BASENAME}" --checksum

echo "[INFO] Step6: Verify upload"
sleep 2
rclone lsf "${REMOTE_PATH}/${BASENAME}" >/dev/null 2>&1 \
  || { echo "[ERROR] upload verification failed"; exit 1; }
echo "[INFO] Uploaded + verified: ${BASENAME}"

echo "[INFO] Step7: Keep last ${KEEP}"
FILES=$(rclone lsf "$REMOTE_PATH" --files-only | sort -r)
COUNT=$(echo "$FILES" | grep -c . || true)
if (( COUNT > KEEP )); then
  echo "$FILES" | tail -n +$((KEEP+1)) | while read -r f; do
    echo "[INFO] deleting old: $f"
    rclone deletefile "${REMOTE_PATH}/${f}"
  done
fi

echo "[INFO] Backup complete"
