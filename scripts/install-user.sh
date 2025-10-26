#!/usr/bin/env bash
set -euo pipefail

# Usage:
# DOTFILES_DIR="$HOME/Projects/dotfiles" TARGET_DIR="$HOME" ./scripts/install-user.sh

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="$HOME/.local/bin"
BIN_PATH="$BIN_DIR/stowd"
LOG_DIR="$HOME/Library/Logs"
PLIST_SRC="$REPO_DIR/launchd/com.orshemtov.stowd.plist"
PLIST_DST="$HOME/Library/LaunchAgents/com.orshemtov.stowd.plist"

: "${DOTFILES_DIR:?Set DOTFILES_DIR to your dotfiles repo path}"
: "${TARGET_DIR:=$HOME}"

mkdir -p "$BIN_DIR" "$LOG_DIR" "$HOME/Library/LaunchAgents"

echo "Building stowd → $BIN_PATH"
GOFLAGS=${GOFLAGS:-}
(cd "$REPO_DIR" && go build -o "$BIN_PATH")

TMP_PLIST="$(mktemp)"
sed -e "s|__ABS_DIR__|$REPO_DIR|g" \
  -e "s|__BIN_PATH__|$BIN_PATH|g" \
  -e "s|__DOTFILES_DIR__|$DOTFILES_DIR|g" \
  -e "s|__TARGET_DIR__|$TARGET_DIR|g" \
  -e "s|__LOCAL_BIN__|$BIN_DIR|g" \
  -e "s|__LOG_DIR__|$LOG_DIR|g" \
  "$PLIST_SRC" >"$TMP_PLIST"

cp "$TMP_PLIST" "$PLIST_DST"

# (Re)load the agent (modern launchctl flow)
launchctl bootout "gui/$(id -u)/com.orshemtov.stowd" >/dev/null 2>&1 || true
launchctl bootstrap "gui/$(id -u)" "$PLIST_DST"
launchctl enable "gui/$(id -u)/com.orshemtov.stowd"
launchctl kickstart -k "gui/$(id -u)/com.orshemtov.stowd"

echo "Installed and started com.orshemtov.stowd"
echo "Logs: $LOG_DIR/stowd.out.log  $LOG_DIR/stowd.err.log"
