# Reasonix CLI Reference

<a href="../README.md">README</a>
&nbsp;·&nbsp;
<a href="./CLI.zh-CN.md">简体中文</a>
&nbsp;·&nbsp;
<a href="./GUIDE.md">Guide</a>

This reference covers interactive sessions, one-shot automation, session
resume, permission flags, and the most useful in-session commands. For provider
configuration, plugins, and sandbox policy, see the [Guide](./GUIDE.md).

## Start a session

```sh
reasonix
reasonix --model deepseek-pro
reasonix --profile delivery --effort high
reasonix --dir /path/to/project
```

Running `reasonix` without a subcommand starts the interactive terminal UI. Use
`reasonix setup` first when no provider is configured.

| Flag | Purpose |
| --- | --- |
| `--model NAME` | Select a configured provider or `provider/model` reference. |
| `--profile economy\|balanced\|delivery` | Select the runtime work profile. |
| `--effort LEVEL` | Override reasoning effort for this session. |
| `--max-steps N` | Set a one-off maximum tool-call round budget; `0` uses automatic execution. |
| `--dir PATH` | Change the workspace root before loading config and tools. |
| `--add-dir PATH` | Add another writable tool directory; repeat for multiple directories. |
| `-c`, `--continue` | Resume the most recent session. |
| `-r`, `--resume [QUERY]` | Open the session picker, or resume a matching session. |
| `--copy` | Continue in a writable copy of the resumed session. |
| `--allowed-tools RULES` | Add session-only permission allow rules. Repeatable; `--allowedTools` is an alias. |
| `--permission-mode MODE` | Start with a specific permission posture. |
| `--yolo` | Start in YOLO mode; alias for `--dangerously-skip-permissions`. |

Flags may appear before or after the prompt where applicable.

## Configure providers

```sh
reasonix setup                    # manage the user-global config
reasonix setup --local            # manage ./reasonix.toml
reasonix setup /path/to/config.toml
```

In an interactive terminal, `reasonix setup` is a staged provider manager. It
lists configured providers and lets you:

- add OpenAI-compatible or Anthropic-compatible providers;
- edit endpoints and model lists;
- update API keys or test the connection and refresh models;
- choose the default model; and
- remove providers.

Choose **Save and exit** to review and confirm the pending operations. Canceling
discards them. Setup reloads the latest config while saving: unrelated desktop
or CLI changes are retained, while an overlapping change is reported as a
conflict instead of being overwritten.

Provider definitions contain only the `api_key_env` variable name. Key values
are stored in the shared Reasonix home `.env`, even with `--local`. When a
variable name is already used by another provider, setup asks whether to share
that credential; choose a different variable name when the providers use
different keys. Providers added or removed through setup are also added to or
removed from desktop provider access, so the same models are available in the
desktop app.

## One-shot and automation

Use `-p` / `--print` when a script needs only the final answer:

```sh
reasonix -p "summarize this repository"
reasonix -p "summarize this repository" --output-format json
reasonix run "implement the TODOs in main.go"
echo "explain this code" | reasonix run
```

`reasonix run` keeps the normal streamed terminal presentation unless `-p` or a
structured output format is selected. It also accepts `--model`, `--profile`,
`--max-steps`, `--effort`, `--dir`, `--add-dir`, `--continue`, `--resume PATH`,
`--copy`, `--allowed-tools`, and `--permission-mode`.

### Output formats

| Format | Behavior |
| --- | --- |
| `text` | Human-readable text. With `-p`, prints only the final answer. |
| `json` | Emits one final result object. |
| `stream-json` | Emits one shared `eventwire` JSON object per line, followed by the final result object. |

```sh
reasonix -p "list the risky changes" --output-format text
reasonix -p "summarize the diff" --output-format json
reasonix run "run the tests" --output-format stream-json
```

The final structured object has this shape:

```json
{
  "type": "result",
  "subtype": "success",
  "is_error": false,
  "duration_ms": 123,
  "num_turns": 1,
  "result": "...",
  "session_id": "...",
  "total_cost_usd": 0,
  "usage": {
    "input_tokens": 0,
    "output_tokens": 0,
    "cache_read_input_tokens": 0,
    "cache_creation_input_tokens": 0
  }
}
```

Execution failures use `subtype: "error_during_execution"` and
`is_error: true`. Structured modes keep runtime errors in JSON instead of also
printing a duplicate human-readable error.

## Resume sessions

```sh
reasonix --continue
reasonix --resume
reasonix --resume provider-config
reasonix --resume <session-id>
reasonix --resume provider-config --copy
```

- `--continue` resumes the newest saved session immediately.
- Bare `--resume` opens the searchable picker in an interactive terminal.
- `--resume QUERY` accepts an exact session ID or path, or a unique title or
  preview substring. Missing and ambiguous matches fail with a descriptive
  error.
- `--resume=true` and `--resume=false` remain accepted for compatibility.
- `--copy` leaves the original transcript untouched and continues in a new
  writable session. Use it when another Reasonix process owns the original.

For one-shot runs, `reasonix run --resume PATH "task"` accepts a session file
path. Session leases prevent the desktop app and CLI from writing the same
transcript concurrently.

## Permissions

```sh
reasonix --permission-mode plan
reasonix --permission-mode acceptEdits
reasonix -p "run the focused tests" --allowed-tools "Bash(go test ./...)"
reasonix --allowed-tools "Bash(git *) Edit"
reasonix --allowed-tools "Bash(go test ./...)" --allowed-tools read_file
```

| Mode | Behavior |
| --- | --- |
| `manual`, `ask` | Ask for ordinary approval decisions. |
| `auto` | Automatically approve normal fallback operations while preserving explicit ask and deny rules. |
| `acceptEdits` | Allow file-editing tools; this is not full Auto mode. |
| `dontAsk` | Deny unapproved requests without opening an approval prompt. |
| `plan` | Start the plan-first workflow; tool calls still use the active permissions and sandbox. |
| `bypassPermissions` | Bypass approval prompts; equivalent to YOLO. |

`--allowed-tools` is a session permission override, not a provider tool-schema
filter. Rules may be comma- or space-separated, and the flag is repeatable.
Configured deny rules always win over command-line allow rules.

In non-interactive runs (`reasonix run` / `-p`) there is no prompt to answer, so
each mode resolves without blocking: `ask`, `manual`, and `acceptEdits` keep run
autonomy and let ordinary approval decisions proceed; `auto` still auto-approves
the normal fallback but denies a command that matches an explicit ask rule rather
than running it unattended; `dontAsk` denies; and `bypassPermissions` runs
everything except tools that always require fresh human approval (memory, plan,
sandbox escape, managed config write).

## Additional directories

```sh
reasonix --add-dir ../shared
reasonix -p "update both projects" \
  --add-dir ../frontend \
  --add-dir ../backend
```

Relative paths resolve from the workspace root and must already exist as
directories. Reasonix resolves symlinks, removes duplicates, and extends the
file-writer and sandboxed Bash write boundaries for the session. These additions
are runtime-only and are not written to configuration.

## Interactive controls

The `/model`, `/provider`, and `/resume` commands use searchable pickers.
Approval prompts use the same row-selection behavior while retaining their
single-key shortcuts.

| Key | Action |
| --- | --- |
| `Up` / `Down`, `Ctrl+P` / `Ctrl+N` | Move through picker or approval rows. |
| `j` / `k` | Move while the search is empty; after search input starts, enter `j` / `k` as query text. |
| Type | Filter a searchable picker. |
| `Enter` | Select the highlighted row. |
| `Esc` | Cancel the current picker or approval. |
| `y` / `a` / `p` / `n`, number keys | Use the matching approval action. |
| `Shift+Tab` | Cycle `Ask → Auto → Plan → Ask`. |
| `Ctrl+Y` | Toggle YOLO independently of the composer-mode cycle. |

The responsive footer keeps interaction state on the left and, when space
allows, places model, effort, and work mode on the right. Its second row shows
available repository and session telemetry such as cache hit rate, context use,
compaction headroom, background jobs, and balance. `ready` means the composer is
idle; that slot changes when a picker, approval, image paste, shell mode, or
other interaction needs attention. Narrow terminals move or compact complete
groups instead of cutting labels in half. Visible labels and work-mode values
follow `/language`.

Use `/theme auto|light|dark` to select the terminal background mode, or choose a
named accent from `/theme`. Both composer borders, the insertion cursor,
selection, scrollbar, and footer use the active CLI theme. See
[Keyboard shortcuts](./GUIDE.md#keyboard-shortcuts) for transcript navigation,
multiline input, rewind, and clipboard controls.

Clipboard actions are deliberately split by content type. Local transcript
and composer selections use the native system clipboard and report success only
after that write completes; SSH falls back to an explicitly labelled OSC 52
request. Text paste remains the terminal's bracketed-paste action (`Cmd+V` on
macOS and the terminal's configured shortcut elsewhere). While Reasonix owns the
mouse in a local session, right-click with no selection reads clipboard text
through the same paste path; right-click with a selection copies it. Over SSH,
use the terminal paste shortcut because the remote process cannot read the local
clipboard; `/mouse` restores the terminal's native right-click menu. Image paste
is application-owned: use `Ctrl+V` on macOS/Linux, `Alt+V` on Windows, or
`/paste-image`; the footer shows `Pasting image…` until the attachment token is
ready.

## In-session commands

Type `/help` in an interactive session for the complete command list. Slash
completion, help, dispatch, and aliases are generated from the same registry, so
the displayed list matches the commands the TUI accepts.

| Command | Purpose |
| --- | --- |
| `/model` | Search configured models and switch the active model. |
| `/provider` | Choose a provider, then choose one of its configured models. |
| `/resume` | Search recent sessions and switch to one. |
| `/status` | Show model, effort, cache, Git, background jobs, and profile or balance details. |
| `/work-mode [economy\|balanced\|delivery]` | View or change the runtime profile; `/profile` is an alias. |
| `/theme [auto\|light\|dark\|style]` | View or change the CLI background mode and accent palette. |
| `/paste-image` | Read a clipboard image and insert an editable attachment token. |
| `/mouse` | Toggle in-app mouse selection, scrollbar, and wheel handling. |
| `/effort` | View or change reasoning effort. |
| `/output-style` | Select an answer style. |
| `/verbose` | Toggle expanded reasoning display. |
| `/sandbox` | Inspect sandbox status. |
| `/goal` | Start, inspect, or clear a long-running goal. |
| `/mcp`, `/skills`, `/hooks`, `/memory` | Inspect and manage extensions or memory. |
| `/rewind` | Restore conversation and/or code to an earlier turn. |
| `/tree`, `/branch`, `/switch` | Inspect or navigate conversation branches. |

Switching model, effort, or work mode rebuilds the runtime while preserving the
active conversation, session-scoped permission overrides, additional directory
access, and session ownership.
