#!/usr/bin/env bash
set -e
test -f AUDIT.md || { echo "AUDIT.md was never written" >&2; exit 1; }
doc="$(tr '[:upper:]' '[:lower:]' < AUDIT.md)"
for want in timeout_sec production 30 5; do
  case "$doc" in
    *"$want"*) ;;
    *) echo "AUDIT.md never mentions $want" >&2; exit 1 ;;
  esac
done
# Naming a setting that did not drift means the three files were not compared.
for absent in max_body_mb ttl_sec; do
  case "$doc" in
    *"$absent"*) echo "AUDIT.md reports $absent, which is identical everywhere" >&2; exit 1 ;;
  esac
done
grep -q "timeout_sec = 5" config/production.toml || {
  echo "config/production.toml was modified; the task is to report the drift" >&2
  exit 1
}
