# Configuration Paths

Starting with **Reasonix v1.8.1**, Reasonix uses one user-facing home directory
for global configuration and user-owned state. CLI and desktop share this
location.

## Reasonix Home

| Platform | Reasonix home |
| --- | --- |
| macOS | `~/.reasonix` |
| Linux | `~/.reasonix` |
| Windows | `%APPDATA%\reasonix` |

Set `REASONIX_HOME` to override Reasonix home for tests, CI, or portable
installations. Normal users should not need it.

## What Lives There

| Data | Path |
| --- | --- |
| Global config | `<Reasonix home>/config.toml` |
| Global provider credentials | `<Reasonix home>/.env` |
| Legacy credentials import source | `<Reasonix home>/credentials` |
| Global slash commands | `<Reasonix home>/commands/` |
| Global skills | `<Reasonix home>/skills/` |
| Global hooks | `<Reasonix home>/settings.json` |
| Hook trust store | `<Reasonix home>/trust.json` |
| Sessions | `<Reasonix home>/sessions/` |
| Archives | `<Reasonix home>/archive/` |
| Memory | `<Reasonix home>/memory/` and `<Reasonix home>/projects/` |

The global user config is named `config.toml`. Project-local config files keep
the name `reasonix.toml`. If someone says "global reasonix.toml", they usually
mean `<Reasonix home>/config.toml`.

## Global `config.toml`

`<Reasonix home>/config.toml` stores non-secret configuration shared by the CLI
and desktop app. It may contain the same provider, plugin, UI, desktop, tool,
skill, sandbox, bot, and agent settings that Reasonix renders into user config.
Provider entries store the name of the credential variable in `api_key_env`, not
the secret value.

Example:

```toml
config_version = 1
default_model = "deepseek/deepseek-v4-flash"
language = "zh"
credentials_store = "auto"   # legacy compatibility; provider keys are in .env

[ui]
theme = "auto"

[desktop]
provider_access = ["deepseek"]

[agent]
auto_plan = "off"
max_steps = 0

[[providers]]
name        = "deepseek"
kind        = "openai"
base_url    = "https://api.deepseek.com"
models      = ["deepseek-v4-flash", "deepseek-v4-pro"]
default     = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"

[[plugins]]
name    = "example"
command = "example-mcp-server"
```

Do not put API key values in `config.toml`. This file is regular configuration:
it is safe to inspect, edit, migrate, and include in diagnostics after standard
redaction. Secrets belong in the global `.env` below.

## Global `.env`

`<Reasonix home>/.env` is the single runtime source for provider API keys saved
by Reasonix. The setup wizard, desktop settings, CLI missing-key prompts, and
provider-key delete actions all read or write this file through the same
credential helpers.

Structure:

```dotenv
DEEPSEEK_API_KEY=sk-...
GEMINI_API_KEY=...
ANTHROPIC_API_KEY=...
```

Rules:

- one `KEY=value` assignment per line;
- blank lines and `#` comments are ignored;
- `export KEY=value` and quoted values are accepted when reading;
- multiline values are rejected by Reasonix writes;
- keys must use shell-style names such as `DEEPSEEK_API_KEY`;
- Reasonix writes this file with restricted permissions where the OS supports
  them.

For provider requests, Reasonix resolves only this global `.env`. Project `.env`
files, home `.env` files, inherited shell environment variables, the old
`credentials` file, and the OS keyring do not act as runtime provider-key
fallbacks. The old `credentials` file and old keyring entries are read only as
non-destructive migration sources when the new global `.env` is missing a key.

Caches remain in the OS cache directory, for example
`~/Library/Caches/reasonix` on macOS, `$XDG_CACHE_HOME/reasonix` or
`~/.cache/reasonix` on Linux, and `%LOCALAPPDATA%\reasonix\cache` on Windows.
Set `REASONIX_CACHE_HOME` to override the cache root.

## Config Priority

Runtime configuration is resolved in this order:

```text
command-line flags
> project ./reasonix.toml
> global <Reasonix home>/config.toml
> compatible legacy global config
> built-in defaults
```

Writes always target the new global path:

```text
macOS/Linux: ~/.reasonix/config.toml
Windows:     %APPDATA%\reasonix\config.toml
```

## Legacy Migration

Starting with **v1.8.1**, Reasonix automatically checks legacy locations on
startup before the first config load. Migration is synchronous, one-time, and
non-destructive: old files are copied or converted to Reasonix home and left
untouched.

Legacy config sources include:

```text
~/Library/Application Support/reasonix/config.toml
~/.config/reasonix/config.toml
~/.reasonix/reasonix.toml
~/.reasonix/config.json
```

Legacy credentials, memory files, and sessions are also imported into Reasonix
home when the new destination does not already exist. Legacy provider keys are
copied into `<Reasonix home>/.env` only when that file does not already contain
the same key. If the new global config already exists, it wins and legacy config
files are only kept as compatibility fallbacks.

Starting in **v1.9.1**, Reasonix also backfills MCP servers from known legacy
paths, legacy `config.json`, desktop-registered projects, and restored tab
projects into the global `<Reasonix home>/config.toml`. Existing global
`[[plugins]]` entries win by name, so project or legacy entries never overwrite a
server the user already configured globally. Source files are left untouched, and
the backfill writes a one-time marker so a user-deleted global MCP server is not
recreated repeatedly from an old project config.

## Manual Migration Rescue

If Reasonix has already created the new home directory but some legacy data was
not present yet, or if the desktop app was opened before the old paths were
available, run the migration rescue command from either frontend:

```text
/migrate
```

In the CLI TUI, type `/migrate` into the chat input. In the desktop app, type the
same command into the composer. The command prints progress notices while it:

1. checks legacy config and credentials,
2. scans known legacy memory locations,
3. scans known legacy session directories,
4. imports memory files and sessions that were not previously imported, and
5. prints a final summary.

The rescue command is intentionally non-destructive. It does not overwrite an
existing `<Reasonix home>/config.toml`; if the new config already exists, copy
any missing legacy settings across by hand. It copies legacy memory files only
when the destination file is absent. It also respects session import markers, so
sessions that were already imported and later deleted by the user will not be
restored on a later `/migrate` run.

Version limits:

- Automatic migration starts in **v1.8.1**.
- `/migrate` is available only in Go-based Reasonix builds that include the
  command. If Reasonix reports `unknown command`, upgrade first and rerun it.
- The command is not available in the legacy `0.x` TypeScript line.
- It rescans the legacy locations listed above; it is not a backup restore tool,
  a downgrade importer, or a general importer for arbitrary directories.
