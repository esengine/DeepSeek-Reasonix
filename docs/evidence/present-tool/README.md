# `present` tool acceptance evidence

Date: 2026-09-13
Source: current `fix/harness-chat-transcript` worktree

## End-to-end UI replay

The production transcript path replays a completed turn containing a trusted
built-in `present` result for an HTML file and a Markdown file. The replay
asserts that the successful tool result stays inside the collapsed process,
while both deliverables remain mounted after the final answer as independent
cards.

- [Chromium deliverable cards](./chromium-presented-files.png)
- [WebKit deliverable cards](./webkit-presented-files.png)
- [Electron deliverable cards](./electron-presented-files.png)
- Raw measurements: [Chromium](./chromium-results.json),
  [WebKit](./webkit-results.json), [Electron](./electron-results.json)

## Measured production-path results

| Host | 240-turn input P95 | 1,000-turn input P95 | 1,000-turn longest task | session switch P95 | anchor drift |
|---|---:|---:|---:|---:|---:|
| Chromium 153 | 36.2 ms | 128.7 ms | 269 ms | 38.1 ms | 0 px |
| WebKit 26.6 | 45 ms | 131 ms | unavailable in WebKit | 56 ms | 0 px |
| Electron 44.2 | 35.3 ms | 52.4 ms | 277 ms | 28 ms | 0 px |

All measured values satisfy the 200 ms input, 300 ms switch, 500 ms long-task,
and 2 px reading-anchor limits. Chromium reported 225,980 bytes of released
heap growth after the switch loop, below the 20 MiB limit. WebKit does not
expose the Long Tasks API in this runner, so no WebKit long-task claim is made.

## Automated checks completed

- Frontend typecheck, hook lint, production build, CSS/theme/layer contracts,
  and bundle budgets.
- Transcript, workspace, remote-session, lifecycle, pagination, and long-history
  suites, including the 240-turn and 1,000-turn browser paths.
- Built-in tool, provider projection, transcript, event wire, Serve history,
  agent, controller, boot, and product-document Go suites.
- Race checks for the tool/provider/transcript/event path and selected desktop
  file-preview/remote-present actions.
- Trusted-path regression checks, including current-policy revocation, remote
  route-generation fencing, and source-host absolute-path resolution.
- Electron typecheck and 192 shell tests, including a regression assertion that
  Node integration is disabled in guest frames and workers.

## Platform limits of this run

Chromium, WebKit, and Electron were executed on macOS arm64. Windows sandbox
logic is covered by automated tests but was not executed on a Windows host in
this run. The legacy native WebView host, a live SSH server, native Office apps,
and every host-specific audio/video codec were not manually verified here.
