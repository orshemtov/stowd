#!/usr/bin/env bash
set -euo pipefail
PLIST_DST="$HOME/Library/LaunchAgents/com.orshemtov.stowd.plist"
launchctl bootout "gui/$(id -u)/com.orshemtov.stowd" || true
rm -f "$PLIST_DST"
echo "Uninstalled com.orshemtov.stowd"
