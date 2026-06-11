#!/usr/bin/env bash
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
desktop_dir="$root/desktop"

vite_port="${REASONIX_DESKTOP_VITE_PORT:-}"
if [[ $# -gt 0 && "$1" =~ ^[0-9]+$ ]]; then
  vite_port="$1"
  shift
fi

if [[ -z "$vite_port" ]]; then
  branch="$(git -C "$root" branch --show-current || true)"
  if [[ -z "$branch" ]]; then
    branch="$(git -C "$root" rev-parse --short HEAD)"
  fi
  key="$root:$branch"
  hash="$(printf "%s" "$key" | cksum | awk '{print $1}')"
  vite_port=$((5173 + (hash % 100)))
fi

if (( vite_port < 1024 || vite_port > 36000 )); then
  echo "dev: Vite port must be between 1024 and 36000, got $vite_port" >&2
  exit 2
fi

wails_port="${REASONIX_DESKTOP_WAILS_PORT:-$((vite_port + 29000))}"

export REASONIX_DESKTOP_VITE_PORT="$vite_port"
export REASONIX_DEV=1
# pnpm honors npm_config_* not PNPM_CONFIG_*; suppress headless TTY purge prompt + age gate.
export npm_config_confirm_modules_purge=false
export npm_config_minimum_release_age=0

echo "Reasonix desktop dev"
echo "  worktree: $root"
echo "  vite:     http://127.0.0.1:$vite_port"
echo "  wails:    127.0.0.1:$wails_port"

cd "$desktop_dir"
exec wails dev -nosyncgomod -devserver "127.0.0.1:$wails_port" "$@"
