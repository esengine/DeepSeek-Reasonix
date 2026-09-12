# Chat content host validation evidence

This directory contains the raw replay results and screenshots captured for the
2026-09-13 chat content-host validation.

## Hosts

| Host | Raw result | Key screenshots |
|---|---|---|
| Chromium 153 | [chromium-results.json](chromium/chromium-results.json) | [chat](chromium/chromium-chat.png), [collapsed process](chromium/chromium-weather-collapsed.png), [expanded process](chromium/chromium-weather-expanded.png), [tool details](chromium/chromium-details.png), [presented files](chromium/chromium-presented-files.png), [turn navigation](chromium/chromium-turn-navigation.png) |
| WebKit 26.6 | [webkit-results.json](webkit/webkit-results.json) | [chat](webkit/webkit-chat.png), [collapsed process](webkit/webkit-weather-collapsed.png), [expanded process](webkit/webkit-weather-expanded.png), [tool details](webkit/webkit-details.png), [presented files](webkit/webkit-presented-files.png), [turn navigation](webkit/webkit-turn-navigation.png) |
| Electron 44.2 | [electron-results.json](electron/electron-results.json), [42-scenario layout output](electron/electron-layout.txt) | [chat](electron/electron-chat.png), [collapsed process](electron/electron-weather-collapsed.png), [expanded process](electron/electron-weather-expanded.png), [tool details](electron/electron-details.png), [presented files](electron/electron-presented-files.png), [turn navigation](electron/electron-turn-navigation.png) |

Each replay covers the 240-turn and 1,000-turn fixtures, cumulative history
loading, process folding, tool inspection, file cards, turn navigation, reading
anchors, session switching, and interactive input sampling. Electron geometry
validation separately passed all 42 layout scenarios.

The summarized thresholds and unverified platform cases are recorded in
[CHAT_CONTENT_HOST.md](../../CHAT_CONTENT_HOST.md) and
[CHAT_CONTENT_HOST.zh-CN.md](../../CHAT_CONTENT_HOST.zh-CN.md).
