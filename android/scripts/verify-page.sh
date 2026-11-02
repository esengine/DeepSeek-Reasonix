#!/usr/bin/env bash
#
# Reproduce what `Kernel.kt` does, on this machine, against the page that is
# actually packaged into the APK.
#
# This is the closest thing to an end-to-end test that runs without a phone: the
# launch flags, the token file, the port file and the `--page` directory are the
# same ones the Kotlin shell passes, and the page is read out of
# `app/src/main/assets/page` rather than the frontend's own `dist/`. If this
# serves a working interface, the only untested part is Android itself.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
android="$here/.."
page="$android/app/src/main/assets/page"
kernel="${KERNEL_BIN:-$android/../reasonix-verify.exe}"

work="${VERIFY_DIR:-/tmp/reasonix-verify}"
rm -rf "$work"
mkdir -p "$work/home/remote"

if [ ! -f "$kernel" ]; then
  echo "no runnable kernel; build one first:" >&2
  echo "  (cd .. && go build -o reasonix-verify.exe ./cmd/reasonix)" >&2
  exit 1
fi

if [ ! -f "$page/index.html" ]; then
  echo "no packaged page: $page/index.html" >&2
  exit 1
fi

# 32 random bytes as hex — the same shape `Kernel.mintToken` writes.
token=$(od -An -tx1 -N32 /dev/urandom | tr -d ' \n')
# The kernel refuses a token file other users can read.
(umask 077; printf '%s\n' "$token" > "$work/home/remote/launch.token")

export REASONIX_HOME="$work/home"
export HOME="$work/home"
export TMPDIR="$work"

echo "kernel: $kernel"
echo "page: $page"
echo "token: ${token:0:8}... (${#token} chars)"

"$kernel" serve \
  --addr 127.0.0.1:0 \
  --auth token \
  --token-file "$work/home/remote/launch.token" \
  --port-file "$work/home/remote/serve.port" \
  --pid-file "$work/home/remote/serve.pid" \
  --no-open \
  -page "$page" \
  > "$work/kernel.log" 2>&1 &
echo $! > "$work/kernel.pid"

# Wait for the port file the kernel writes after binding, which is exactly the
# wait `Kernel.awaitPort` performs.
for _ in $(seq 1 150); do
  if [ -s "$work/home/remote/serve.port" ]; then break; fi
  if ! kill -0 "$(cat "$work/kernel.pid")" 2>/dev/null; then
    echo "the kernel exited; log:" >&2
    cat "$work/kernel.log" >&2
    exit 1
  fi
  sleep 0.2
done

if [ ! -s "$work/home/remote/serve.port" ]; then
  echo "the kernel reported no listening address; log:" >&2
  cat "$work/kernel.log" >&2
  exit 1
fi

port=$(cat "$work/home/remote/serve.port")
echo "listening on: $port"

# The two things the WebView will do: fetch the page, and exist on the origin.
probe=$(curl -s -o /dev/null -w '%{http_code}' "http://$port/")
echo "GET /  -> $probe"
if [ "$probe" != "200" ]; then
  echo "the page did not answer; log:" >&2
  cat "$work/kernel.log" >&2
  exit 1
fi

cat > "$work/entry.txt" <<EOF
origin=http://$port
entry=http://$port/#token=$token
token=$token
EOF

echo "entry written: $work/entry.txt"
echo "kernel log (first 10 lines):"
head -10 "$work/kernel.log"
