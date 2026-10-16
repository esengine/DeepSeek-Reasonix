#!/usr/bin/env bash
#
# Install the built APK on a connected phone and show its log.
#
# `adb` comes from the Android SDK rather than this project, because the SDK is
# already how the app is built. Point ANDROID_HOME at it, or have `adb` on PATH.
#
# Usage: ./scripts/run.sh [debug|release]
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
android="$here/.."
variant="${1:-release}"

adb="${ADB:-${ANDROID_HOME:-$HOME/AndroidSDK}/platform-tools/adb}"
if ! command -v "$adb" >/dev/null 2>&1; then
  adb="adb"
fi

apk_dir="$android/app/build/outputs/apk/$variant"
apk=$(find "$apk_dir" -name '*.apk' -maxdepth 1 2>/dev/null | head -n1)
if [ -z "$apk" ]; then
  echo "没有找到 $variant 安装包，先运行 ./scripts/build.sh $variant" >&2
  exit 1
fi

echo "正在安装 $apk …"
"$adb" install -r "$apk"

# The device count is checked before the app is started so an emulator-less
# machine gets "no devices" rather than a launch failure that reads like a bug.
if [ "$("$adb" devices | grep -c 'device$')" -eq 0 ]; then
  echo "没有已连接的设备，跳过启动。" >&2
  exit 0
fi

package="io.reasonix.mobile.app"
[ "$variant" = debug ] && package="io.reasonix.mobile.app.debug"
"$adb" shell monkey -p "$package" -c android.intent.category.LAUNCHER 1 >/dev/null

echo
echo "正在跟随日志（Ctrl-C 退出）…"
"$adb" logcat -v color "ReasonixKernel:V" "AndroidRuntime:E" "*:S"
