---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-09-25
---

# Reasonix 2.x roadmap

## Purpose

This page tracks what Reasonix 2.x still has to deliver, how it ships, and which decisions are still open.

- Why there are two release lines: the [version roadmap announcement](https://github.com/esengine/DeepSeek-Reasonix/discussions/10748).
- How to move between them: [Moving from 1.x to 2.x](./MIGRATING.md).

## Release lines

| Line | Branch | Status | Ships |
| --- | --- | --- | --- |
| 2.x | `studio` | Active development | Reasonix Studio and the `reasonix` CLI, from `studio-v2.*` tags |
| 1.x | `main-v2` | Maintenance / stable | The 1.x desktop app. The last 1.x CLI on npm and Homebrew is 1.39.0. |

## How 2.x ships

| ID | Rule |
| --- | --- |
| R1 | A `studio-v2.*` tag builds Studio for macOS, Windows and Linux, and `reasonix` CLI archives for `darwin`, `linux` and `windows` on `amd64` and `arm64` (`.github/workflows/release-studio.yml`). |
| R2 | Studio updates itself in place from the release it was installed from ([Studio release runbook](./STUDIO_RELEASE.md)). |
| R3 | The same release publishes the CLI to npm and to the `esengine/reasonix` Homebrew tap. A stable version such as `2.21.0` moves npm `latest`; a candidate skips Homebrew. |
| R4 | 1.x no longer publishes the CLI to npm, Homebrew or the CLI update pointers. |
| R5 | The next stable `studio-v2.*` release MUST NOT be tagged until every gate in [General availability](#general-availability) is met, because R3 makes it the release that moves `npm i -g reasonix` and `brew install` from 1.x to 2.x. |
| R6 | Planned, not in place: stable releases follow a weekly train. Builds publish to npm `next` as they land, and one fixed day each week promotes the previous week's `next` to `latest` unless a P0 or P1 regression is open. Today every stable tag moves `latest` at once. |
| R7 | Planned, not in place: Studio offers a `stable` and a `latest` update channel that follow the same two tags. |

## General availability

Stable `studio-v2.*` releases are GitHub latest. The release that moves npm and Homebrew from 1.x to 2.x is the next stable one after every gate below holds, and until then it MUST NOT be tagged.

| ID | Gate | Status |
| --- | --- | --- |
| G1 | Every row of [Terminal takeover](#terminal-takeover) is done | Open |
| G2 | W1a and W1b are done, so a person moving from npm 1.x finds their conversations in 2.x | Met |
| G3 | Session and config formats carry a version and a migration path, and are frozen for the 2.x line | Open |
| G4 | Two consecutive weeks of `next` builds with no open P0 or P1 issue | Open |
| G5 | Every porting-list row marked required (W3) is done | Open |
| G6 | Signed, self-updating builds on macOS, Windows and Linux | Met |

## Terminal takeover

The 2.x terminal UI (`reasonix tui`) replaces the 1.x chat screen for people who work in a terminal.

| ID | Item | Status |
| --- | --- | --- |
| T1 | Terminal UI on the in-process kernel: streaming, approvals, questions, queue and steer, `!` shell commands, completion, task list | Done |
| T2 | Look and keys of the 1.x chat screen: banner, framed composer, mode tag, footer telemetry, per-request usage, approval and question panels, i18n | Done |
| T3 | Full screen by default with scrollbar, wheel, drag-select copy, `/mouse`; `--inline` for the terminal's own scrollback | Done |
| T4 | `-r` session picker and `/resume`; system notifications; Ctrl+B for long shell output | Done |
| T5 | Ctrl+V pastes a screenshot (Alt+V on Windows) | Done |
| T6 | Bare `reasonix`, `reasonix -c` and `reasonix -r` start the terminal UI, as they do in 1.x, with 1.x's session options and permission-mode names | Done |
| T7 | Windows checks by hand: IME candidate window position, Alt+V image paste | Not started |
| T8 | Local install test of the npm package and the Homebrew cask before the switch release | Not started |
| T9 | `README.md` and `README.zh-CN.md` name npm and Homebrew as the 2.x CLI channels in the switch release | Not started |

## Other workstreams

| ID | Item | Line | Status |
| --- | --- | --- | --- |
| W1a | 2.x opens sessions 1.x 1.38.2 to 1.38.7 saved (event log schema 2), and continues them in a 2.x session | 2.x | Done |
| W1b | 2.x lists and opens sessions 1.x 1.38.8 or later keep in `sessions-v4`; see [Sessions](./MIGRATING.md#sessions) | 2.x | Done |
| W2 | 1.x stops listing the `.wire.jsonl`, `.adjudication.jsonl` and `.execution.jsonl` files 2.x writes as conversations | 1.x | In review: #10767 |
| W3 | 1.x to 2.x porting list: 1.x fixes, tests and behavior, each marked ported, reimplemented or not carried over | Both | Not started |
| W4 | 2.x contribution and architecture review rules in `CONTRIBUTING.md` | 2.x | Partly done: branch table, dependency direction and cache-first review gate exist |
| W5 | Version note in the 1.x `README.md` on `main-v2` | 1.x | Not started |

## 1.x maintenance

| ID | Rule | Status |
| --- | --- | --- |
| M1 | 1.x takes bug fixes, security fixes and provider or API compatibility only. It gets no new features and no new session, config or storage formats. | Proposed, needs agreement with the 1.x maintainers |
| M2 | 1.x maintenance ends 3 months after 2.x general availability, or on a fixed backstop date, whichever comes first. | Proposed |
| M3 | The backstop date is set by @esengine and published in the version announcement. | Open |
| M4 | A fix that matters on both lines lands on `main-v2` and is recorded in the porting list (W3). | Active |

## Open decisions

These are decided by @esengine and recorded here when made.

| ID | Decision | State |
| --- | --- | --- |
| D1 | The backstop date that ends 1.x maintenance (M2, M3) | Open |
| D2 | Agreement with the 1.x maintainers on the feature and format freeze (M1) | Open |
