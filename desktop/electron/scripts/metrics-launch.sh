#!/usr/bin/env bash
# Single-executable entry for scripts/desktop-shell-metrics.sh: exec the
# Electron binary so the measured pid is the shell itself.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
service="${REASONIX_DESKTOP_SERVICE:-$root/../build/bin/reasonix-desktop-service}"
electron="$(node -e 'process.stdout.write(require("electron"))' 2>/dev/null || true)"
if [ -z "$electron" ]; then electron="$(cd "$root" && node -e 'process.stdout.write(require("electron"))')"; fi
export REASONIX_DESKTOP_SERVICE="$service"
exec "$electron" "$root"
