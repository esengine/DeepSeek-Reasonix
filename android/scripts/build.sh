#!/usr/bin/env bash
#
# One command from a clean checkout to an installable APK.
#
# The order matters and is the whole content of this script: the kernel and the
# page are inputs to packaging, not build steps inside it, so they are produced
# first and the Gradle task only assembles what is already there. Running
# `gradle assembleRelease` directly on a fresh checkout fails on purpose (see
# `requireBuildInputs` in app/build.gradle) rather than producing an APK with no
# kernel in it.
#
# Usage: ./scripts/build.sh [debug|release]     (default: release)
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
android="$here/.."
variant="${1:-release}"

case "$variant" in
  debug|release) ;;
  *) echo "用法：$0 [debug|release]" >&2; exit 2 ;;
esac

"$here/build-kernel.sh"
"$here/sync-page.sh"

cd "$android"

# The wrapper is generated rather than committed as a jar, so a checkout that
# has never been built gets one here. `gradle` itself has to be on PATH for this
# once; after that `./gradlew` is what runs.
if [ ! -f gradlew ]; then
  echo "正在生成 Gradle wrapper…"
  gradle wrapper --gradle-version 8.14.3 --no-daemon
fi

task="assemble$([ "$variant" = release ] && echo Release || echo Debug)"
echo "正在打包 APK（$variant）…"
./gradlew "$task" --no-daemon

apk_dir="$android/app/build/outputs/apk/$variant"
echo
echo "完成。安装包："
find "$apk_dir" -name '*.apk' -maxdepth 1 -print -exec ls -lh {} \;
