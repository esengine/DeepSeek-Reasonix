# Capability diagnostics

<a href="./CAPABILITY_DIAGNOSTICS.zh-CN.md">简体中文</a>
&nbsp;·&nbsp;
<a href="./GUIDE.md">Guide</a>
&nbsp;·&nbsp;
<a href="./PLUGIN_PACKAGES.md">Plugin packages</a>

Reasonix ships a read-only capability diagnostics model shared by the CLI and
desktop **Settings → Diagnostics**. It reports Skills, Commands, Hooks, plugin
packages, MCP servers, and instruction docs (`AGENTS.md` / `REASONIX.md` /
`CLAUDE.md`).

**Write policy**

| Mode | Config files | MCP stats / schema cache | Network / MCP processes |
| --- | --- | --- | --- |
| Static (default) + desktop | Never written (`LoadForRootReadOnly`) | Never written | None |
| CLI `--live` | Never written | **Not written** (`SkipPersistence`) | Starts automatic MCP in an isolated Host |

## How to use (quick start)

| Goal | What to run |
| --- | --- |
| Check this workspace’s skills / hooks / MCP / plugins | `reasonix doctor capabilities` |
| Machine-readable report (CI / support) | `reasonix doctor capabilities --json` |
| Another project root | `reasonix doctor capabilities --root /path/to/project` |
| Probe MCP startup for real (starts third-party servers) | `reasonix doctor capabilities --live --timeout 5s` |
| Ask the agent to walk through config / fix guidance | `/reasonix-guide` in chat, or ask naturally |
| GUI health view | Desktop **Settings → Diagnostics** |

**Default is static and safe:** no network, no MCP child processes. Use `--live`
only when you explicitly want to start automatic MCP servers.

Related (unchanged) doctor commands:

```bash
reasonix doctor                  # env / providers / sandbox snapshot
reasonix doctor session <id>     # support session bundle
reasonix doctor redact-sessions  # redact secrets in session files
```

## Everyday workflows

### 1. “Skill / command is missing or wrong”

```bash
reasonix doctor capabilities --json | jq '.skills.entries, .commands.entries, .issues'
```

Look for:

- `skill.shadowed` / `command.shadowed` — a higher-priority path won
- `skill.disabled` — name is in `[skills].disabled_skills`
- `skill.missing_description` — skill loads but index quality is weak
- `command.read_failed` — unreadable or broken markdown

Then open **Settings → Skills** (or fix the file under `.reasonix/skills` /
`.reasonix/commands`).

### 2. “Project hooks never fire”

```bash
reasonix doctor capabilities | sed -n '/Hooks/,/Plugins/p'
```

If you see `hook.untrusted_project`, trust the workspace in **Settings → Hooks**
(or the CLI trust flow). Matchers are **anchored** regexes: `file` does not match
`read_file`.

### 3. “MCP tools don’t show up”

1. Static first (no side effects):

   ```bash
   reasonix doctor capabilities --json | jq '.mcp.servers, .issues[] | select(.subsystem=="mcp")'
   ```

2. Only if you accept starting third-party servers:

   ```bash
   reasonix doctor capabilities --live --timeout 10s --json
   ```

Common codes: `mcp.command_not_found`, `mcp.invalid_transport`,
`mcp.start_failed`, `mcp.no_tools`. On desktop, prefer **Settings → Diagnostics**
with “Include current session runtime” to read the **active tab Host** without
starting a second Host.

### 4. Ask the agent (`reasonix-guide`)

In an interactive session:

```text
/reasonix-guide
```

or:

```text
My MCP server X is configured but the model never sees its tools — diagnose.
```

The built-in skill is **inline** (`runAs: inline`). It tells the model to prefer:

```bash
reasonix doctor capabilities --json
```

and to use `--live` only after you explicitly allow external MCP. Project or
global skills named `reasonix-guide` override the builtin; you can also hide it
with `[skills].disabled_skills = ["reasonix-guide"]`.

## CLI reference

```bash
reasonix doctor capabilities [--root PATH] [--json] [--live] [--timeout 5s]
```

| Flag | Meaning |
| --- | --- |
| `--root` | Workspace root (default: current directory). Uses `config.LoadForRoot`. |
| `--json` | Write one JSON object to **stdout** only (warnings go to stderr). |
| `--live` | Start **automatic** MCP servers in an isolated Host (may network). |
| `--timeout` | Per-server live timeout, **1s–60s**, default `5s`. Requires `--live`. |

### Modes

| Mode | Behavior |
| --- | --- |
| **Static (default)** | No network; no stdio / HTTP / SSE MCP child processes. |
| **Live (`--live`)** | Stderr risk banner; only servers with automatic start intent; `auto_start=false` → `skipped`; concurrency 4; Host always closed. |

Desktop “include current session runtime” is **not** CLI `--live`: the desktop
only **reads** the active tab Host and never starts MCP.

### Exit codes

| Code | Meaning |
| --- | --- |
| `0` | No `error`-severity issues (warnings/info are allowed) |
| `1` | One or more `error` issues, or live MCP start failures |
| `2` | Bad flags / usage |

Examples:

```bash
# Human-readable, current directory
reasonix doctor capabilities

# Fail CI only on hard errors
reasonix doctor capabilities --json
# shell: exit code 1 if summary.errors > 0

# Live probe with a longer timeout
reasonix doctor capabilities --live --timeout 15s --json 2>live-warn.txt
```

Existing `reasonix doctor`, `doctor session`, and `doctor redact-sessions`
commands keep their own JSON schemas — capability fields are **not** mixed into
those reports.

## Desktop

Open **Settings → Diagnostics**:

| Control | Behavior |
| --- | --- |
| Open page | Loads a **static** report for the active workspace root |
| Refresh | Re-runs collection with the current runtime toggle |
| Copy redacted JSON | Clipboard paste-safe report (paths already redacted) |
| Include current session runtime | Merge connected / failed / deferred / disabled from the **active tab Host** only |
| Open settings (on an issue) | Jumps to MCP / Skills / Plugins / Hooks when `settings_tab` is set |

The page never auto-edits config, auto-trusts projects, auto-enables packages,
or reconnects MCP. Opening Diagnostics does not rebuild the controller or
snapshot the session.

## JSON schema (version 1)

Top-level fields:

- `schema_version` (always `1`)
- `root` (display path)
- `live` (bool)
- `summary` — error/warning/info counts and resource counts
- `instructions`, `skills`, `commands`, `hooks`, `plugins`, `mcp`
- `issues[]` — ordered list of findings

Issue shape:

```json
{
  "severity": "error|warning|info",
  "code": "skill.shadowed",
  "subsystem": "skills",
  "name": "demo",
  "source": "<workspace>/.reasonix/skills/demo/SKILL.md",
  "message": "...",
  "remediation": "...",
  "settings_tab": "skills"
}
```

Stable codes include:

- `skill.shadowed`, `skill.missing_description`, `skill.disabled`
- `command.shadowed`, `command.read_failed`
- `hook.untrusted_project`, `hook.invalid_matcher`, `hook.missing_command`, `hook.malformed_settings`
- `plugin.missing_root`, `plugin.invalid_manifest`, `plugin.compatibility`
- `mcp.invalid_transport`, `mcp.command_not_found`, `mcp.missing_command`, `mcp.missing_url`
- `mcp.start_failed`, `mcp.no_tools`, `mcp.runtime_unavailable`

Array and issue order is deterministic for scripting and tests.

### Severity

| Severity | Meaning | CLI exit |
| --- | --- | --- |
| `error` | Broken config or failed live start | `1` |
| `warning` | Actionable but non-fatal (e.g. untrusted project hooks) | `0` |
| `info` | Shadowing, disabled assets, runtime unavailable | `0` |

## Path and secret safety

Reports rewrite paths as:

- `<workspace>/...` under the diagnosis root
- `~/...` under the user home
- `<external>/basename` for other absolute paths (no full external path)

They never intentionally emit usernames, full external paths, environment
variable **values**, header **values**, tokens, or URL query strings. MCP
entries list env/header **keys** only. Error text that may carry raw HTTP
response bodies or MCP stderr passes through the product-wide secret redactor
(Authorization schemes, Bearer/JWT/vendor tokens, `KEY=value` and JSON
`"key":"value"` credential forms, Cookie/Set-Cookie values) and is truncated to
400 characters. Prefer copying report JSON into issues or chat over pasting raw
config files.

## What is *not* diagnosed here

| Need | Use instead |
| --- | --- |
| Provider keys, proxy, sandbox OS support | `reasonix doctor` |
| Full session transcript for support | `reasonix doctor session <id>` |
| One plugin package only | `reasonix plugin doctor <name>` |
| Interactive MCP list in a chat session | `/mcp` |

## Cache impact

Adding the built-in `reasonix-guide` skill appends **one stable index line** to
the system-prompt skills index after upgrade. The skill body is loaded only on
invocation. Diagnostics itself is not part of the provider prompt.
