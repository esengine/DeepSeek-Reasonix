---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-10-04
---

# Moving from Reasonix 1.x to 2.x

## Purpose

This guide is for people who run Reasonix 1.x and want to try or move to 2.x. It lists what the two lines share, what does not carry over, and the steps to run both on one machine.

- Why there are two release lines: the [version roadmap announcement](https://github.com/esengine/DeepSeek-Reasonix/discussions/10748).
- What 2.x still has to deliver: the [roadmap](./ROADMAP.md).

## Release lines

| | Reasonix 1.x | Reasonix 2.x |
| --- | --- | --- |
| Branch | `main-v2` | `studio` (default) |
| Status | Maintenance / stable | Active development |
| Desktop app | 1.x desktop, from the [download page](https://reasonix.io/?download=desktop#start) | Reasonix Studio, from the [`studio-v2.*` releases](https://github.com/esengine/DeepSeek-Reasonix/releases?q=studio-v&expanded=true) |
| CLI | `npm i -g reasonix`, or `brew install esengine/reasonix/reasonix` | Archives attached to each `studio-v2.*` release |
| Issue label | `v2` | `v3` |

## What the two lines share

Both lines read and write the same Reasonix home: `~/.reasonix` on macOS and Linux, `%APPDATA%\reasonix` on Windows. See [Configuration Paths](./CONFIG_PATHS.md).

| Data | Path | Shared |
| --- | --- | --- |
| Global config | `<home>/config.toml` | Yes. 2.x keeps sections it does not use, such as `[bot]`, unchanged when it rewrites the file. |
| Provider keys | `<home>/.env` | Yes |
| Slash commands, skills, hooks | `<home>/commands/`, `<home>/skills/`, `<home>/settings.json` | Yes |
| Memory | `<home>/memory/`, `<home>/projects/` | Yes |
| Project instructions | `REASONIX.md`, `AGENTS.md`, `CLAUDE.md` in the project | Yes |
| Sessions | `<home>/projects/<project>/sessions/`; 1.x 1.38.8 or later also `sessions-v4/` | Partly. See [Sessions](#sessions). |

## Sessions

Where 1.x keeps a conversation depends on the 1.x version that last saved it.

| Saved by | Kept in | Opens in 2.x |
| --- | --- | --- |
| 1.x before 1.38.2 | `sessions/`, event log schema 1 | Yes |
| 1.x 1.38.2 to 1.38.7 | `sessions/`, event log schema 2 | Yes |
| 1.x 1.38.8 or later | `sessions-v4/<id>/` | Yes. Opening one imports it to `sessions/v4-<id>.jsonl`. |
| 2.x | `sessions/`, event log schema 1 | Yes |

| ID | Rule |
| --- | --- |
| S1 | 2.x never writes a file of a session 1.x saved. Opening one and leaving it changes nothing. |
| S2 | Continuing a `sessions/` 1.x session in 2.x moves it to a new 2.x session with the same title before the first turn runs, and says so. 1.x keeps the original. |
| S3 | 1.x 1.38.8 or later copies an older session into `sessions-v4` when it opens it. The copy under `sessions/` stays as it was, and 2.x still opens it. |
| S4 | A `sessions-v4` conversation is imported to `sessions/v4-<id>.jsonl` the first time 2.x opens it. If 1.x continues it later, 2.x refreshes the import on the next open while the import has no 2.x turns; after it has, the two copies go their own ways. |
| S6 | `reasonix session list --json` lists 2.x transcripts only; a `sessions-v4` conversation appears there once it has been imported. |
| S5 | 1.x lists the `.wire.jsonl`, `.adjudication.jsonl` and `.execution.jsonl` files 2.x writes as extra conversations. Ignore them in 1.x; they belong to the 2.x session with the same name. |

## Commands

| 1.x | 2.x |
| --- | --- |
| `reasonix`, `reasonix -c`, `reasonix -r` | The same, in a terminal. `reasonix tui` is another name for it. |
| `--permission-mode workspace-write` | Auto |
| `--permission-mode danger-full-access` | Yolo |
| `--permission-mode read-only` | Read only (`read-only`). Every write is refused. |
| `--yolo`, `--permission-mode yolo` or `bypassPermissions` | Yolo. In 1.x these meant `workspace-write`; in 2.x they skip ordinary approval prompts. |
| `reasonix run`, `serve`, `web`, `acp`, `mcp`, `setup`, `doctor` | Same names |
| `reasonix bot` | Not in 2.x. The `[bot]` config section stays for 1.x. |
| VS Code extension | Starts the `reasonix` found on `PATH`, so it runs whichever line's CLI comes first there. |

## Behaviour that differs from 1.x

### Trusting a folder

When a folder is trustable, its writes are sandbox-confined and no decision is recorded yet, `reasonix` asks ``Trust this folder? (`reasonix trust --revoke` undoes it) [y/N]`` before the terminal UI starts. 1.x has no such prompt.

- The answer is stored either way.
- `reasonix trust` shows what the folder would run and approves it; `reasonix trust --revoke` undoes it.
- The default mode is Auto where the OS sandbox confines writes and the folder is trusted, and Ask otherwise.

### `reasonix run` and `-p` in a folder that is not trusted

1.x wrote files and ran commands in any folder. In 2.x an untrusted folder has nobody to approve edits and shell commands, so a headless run refuses them. Nothing is loosened: the run now says so instead of looking like success.

- stderr names the refused tools, the code `permission.untrusted_folder` and the remedy.
- The process exits `4` (`3` under `--fail-on-unverified`); the work was not done.
- `--output-format json` and `stream-json` list each refusal in `permission_denials` with its `code` and `remedy`, and set `unverified_by`.

To make a script behave as it did in 1.x, trust the folder once (`reasonix trust --dir <folder>`), or pass `--permission-mode auto` or `--yolo` knowingly for that run. Other exit codes are as in 1.x: `1` for an error, `2` for a usage error.

### Other differences

| Area | 1.x | 2.x |
| --- | --- | --- |
| `reasonix config compact-ratio` | Accepts 30 to 85 | Accepts any percentage above 0 and below 100, so every 1.x value still works. The default differs from 1.x, and the status line reads `to compaction N%`. |
| Unknown `/command` | Sent to the model as an ordinary message, with a notice that names the command | The same. |
| Permission mode cycle | Shift+Tab cycles Workspace, YOLO, Plan, Read only | Shift+Tab cycles Auto, YOLO, Plan, Read only, Ask. Shift+Tab and Ctrl+Y enter YOLO at once, with no confirmation, as in 1.x; starting with `--yolo` or `--permission-mode yolo` asks once the first time. Auto asks first for nested or indirect shell such as `python3 -c '...'`, so expect more prompts. |
| Status line | `workspace@branch` and a compaction threshold | Same row; the threshold reads `to compaction N%`. The footer and turn receipt name the peak or off-peak rate a spend was billed at, as 1.x does, only when the vendor's own schedule priced it. |
| `REASONIX_CHROME` | Path of the browser the browser tool launches | Not read. Set `[browser] executable` in the config; with none set, 2.x looks for Chrome, Edge or Chromium. |
| `REASONIX_SESSION_LOG` | `v1` switched session saves back to the schema-1 writer | Removed. 2.x writes one session format. |
| Default permission mode | Workspace | Auto, 1.x's Workspace, where the OS sandbox confines writes and the folder is trusted; Ask otherwise. See [Trusting a folder](#trusting-a-folder). |
| `/recover-context`, `/continue-checks` | Slash commands | Retired; 2.x has no such command. |
| `/web` | Slash command | Not a slash command in 2.x. Run `reasonix web` from the shell. |
| Studio-only slash commands | None | `/version`, `/feedback`, `/locate`, `/setup` and `/auth`. `/setup` and `/auth` open the setup panel; `/locate` is a built-in read-only skill that returns file and line ranges. |
| `reasonix bot` | IM gateway | Removed. The `[bot]` config section is kept untouched. |

## Steps: run 2.x beside 1.x

1. Download Reasonix Studio for your platform from the latest [`studio-v2.*` release](https://github.com/esengine/DeepSeek-Reasonix/releases?q=studio-v&expanded=true) and install it. Studio updates itself after that.
2. For the 2.x terminal UI, download the `reasonix` archive for your platform from the same release and unpack it into a directory of your choice.
3. Start the 2.x terminal UI from the unpacked directory:

   ```sh
   ./reasonix tui
   ```

4. Report 2.x problems with **Version line: 2.x** in the issue form, and the version from Studio's settings or `reasonix --version`.

## Steps: go back to 1.x

1. Keep using the 1.x desktop app or `reasonix` from npm or Homebrew. Config, keys, skills and memory are the same files.
2. Sessions 2.x saved open in 1.x. 1.x 1.38.8 or later copies them into `sessions-v4` first (rule S3).
