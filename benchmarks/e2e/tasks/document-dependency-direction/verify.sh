#!/usr/bin/env bash
set -e
test -f ARCHITECTURE.md || { echo "ARCHITECTURE.md was never written" >&2; exit 1; }
doc="$(tr '[:upper:]' '[:lower:]' < ARCHITECTURE.md)"
for want in reader normalizer writer; do
  case "$doc" in
    *"$want"*) ;;
    *) echo "ARCHITECTURE.md never mentions $want" >&2; exit 1 ;;
  esac
done
# The direction is the whole question: writer sits above normalizer, which sits
# above reader. A document that lists the three names without the order has not
# read the imports.
case "$doc" in
  *"writer"*"normalizer"*"reader"*) ;;
  *) echo "ARCHITECTURE.md does not state the layering order" >&2; exit 1 ;;
esac
grep -q "from pipeline import reader" pipeline/normalizer.py || {
  echo "pipeline/normalizer.py was modified; the task is to describe it" >&2
  exit 1
}
