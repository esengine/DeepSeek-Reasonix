#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
import urls
assert urls.article_url("Hello, World!") == "/articles/hello-world"
assert urls.article_url("a  b") == "/articles/a-b"
PY
