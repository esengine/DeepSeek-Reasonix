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

When `REASONIX_HOME` is set, the runtime is fully self-contained: all
configuration, state, cache, and data live under that directory tree. Legacy
migration, OS-home convention directory scanning, and all other fallback paths
are skipped so no data leaks in from a system-wide production install.

Advanced test and portable setups may set `REASONIX_STATE_HOME` to move runtime
state such as sessions, archives, and memory. It does not move global config or
provider credentials: those remain under `REASONIX_HOME`. If an older build wrote
provider keys to `REASONIX_STATE_HOME/.env`, Reasonix imports those keys
non-destructively when `<Reasonix home>/.env` is missing them.

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
| Remote-SSH managed known_hosts | `<Reasonix home>/remote/known_hosts` |
| Sessions | `<state root>/sessions/` |
| Archives | `<state root>/archive/` |
| Memory | `<state root>/memory/` and `<state root>/projects/` |

`<state root>` defaults to `<Reasonix home>`. It only differs when
`REASONIX_STATE_HOME` is set.

The global user config is named `config.toml`. Project-local config files keep
the name `reasonix.toml`. If someone says "global reasonix.toml", they usually
mean `<Reasonix home>/config.toml`.

## Global `config.toml`

`<Reasonix home>/config.toml` stores non-secret configuration shared by the CLI
and desktop app. It may contain the same provider, plugin, UI, desktop, tool,
skill, sandbox, bot, and agent settings that Reasonix renders into user config.
Provider entries store the name of the credential variable in `api_key_env`, not
the secret value.

Saved provider and bot credential variables are removed from every
model-controlled child-process environment. The global credential `.env` is
also hidden from Reasonix's file readers, sandboxed shell commands, and MCP
servers; this does not change the visibility of a project's ordinary `.env`.
On Windows, shell commands remain outside an OS sandbox as documented in the
Guide, so approve shell access only for trusted tasks.

Example:

```toml
config_version = 1
default_model = "deepseek/deepseek-v4-flash"
language = "zh"
credentials_store = "auto"   # legacy compatibility; provider keys are in .env

[ui]
theme = "auto"
cursor_shape = "bar"         # CLI/TUI text cursor: underline|block|bar

[desktop]
provider_access = ["deepseek"]

[agent]
auto_plan = "off"

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

`[ui].cursor_shape` affects only the CLI/TUI composer. The default `bar` stays
visible without covering double-width CJK characters; use `block` or
`underline` if you prefer those cursor shapes.

### Custom provider `api_key_env` names

When a custom provider is added from the desktop settings or `reasonix setup`,
Reasonix stores a generated `api_key_env` in `config.toml` and writes the secret
value to the matching key in the global `.env`. The generated name is stable, so
the same provider keeps using the same credential slot after restart.

Reasonix derives the default from the provider name. Names that normalize to
ASCII keep readable env names such as `LOCAL_GATEWAY_API_KEY`; names made
entirely of non-ASCII characters get a stable hash suffix such as
`CUSTOM_d39b9067_API_KEY` so two Chinese provider names do not share
`CUSTOM_API_KEY`. Names beginning with a digit get a `CUSTOM_` prefix so the
generated environment variable remains valid; for example, `9router` becomes
`CUSTOM_9ROUTER_API_KEY`.

In the CLI custom-provider wizard, the provider name is generated from the base
URL first, then the same provider-name rule is applied. For example
`https://token.sensenova.cn/v1` creates provider name
`custom-token-sensenova-cn`, whose default key env is
`CUSTOM_TOKEN_SENSENOVA_CN_API_KEY`. Press Enter to accept that default, or type
an explicit env name such as `CUSTOM_API_KEY` if you intentionally want to share
one credential across providers.

Existing configs are not rewritten on upgrade. If an old custom provider already
uses `CUSTOM_API_KEY`, it will keep working with that key. If several old custom
providers accidentally share `CUSTOM_API_KEY`, edit each provider's
`api_key_env` to a distinct name and save the corresponding API key again.

### Custom provider endpoint URLs

Custom OpenAI-compatible providers normally store an API endpoint in `base_url`.
Reasonix sends chat requests to `base_url + "/chat/completions"` and probes model
discovery candidates such as `/models` and `/v1/models`. If a gateway gives you a
complete chat request URL, set `chat_url`; Reasonix will use it directly and will
not append `/chat/completions`. If model discovery needs a separate address, set
`models_url`.

If a gateway requires vendor-specific top-level request body fields, set
`extra_body`, for example `extra_body = { enable_thinking = true }`. These values
are merged into the OpenAI-compatible chat JSON request body without allowing
core fields such as `model`, `messages`, `tools`, or `stream` to be overridden.

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
# reasonix-cleared OLD_API_KEY
```

Rules:

- one `KEY=value` assignment per line;
- blank lines and `#` comments are ignored;
- `export KEY=value` and quoted values are accepted when reading;
- multiline values are rejected by Reasonix writes;
- keys must use shell-style names such as `DEEPSEEK_API_KEY`;
- `# reasonix-cleared KEY` comments are non-secret tombstones written after a key
  is deleted so legacy stores do not silently re-import it;
- Reasonix writes this file with restricted permissions where the OS supports
  them.

For provider requests, Reasonix resolves only this global `.env`. Project `.env`
files, home `.env` files, inherited shell environment variables, the old
`credentials` file, and the OS keyring do not act as runtime provider-key
fallbacks. Project `.env`, home `.env`, and inherited shell environment values
are not imported into the global credentials file. The old `credentials` file
and old keyring entries are read only as non-destructive migration sources when
the new global `.env` is missing a key. Project `.env` files are still read as
workspace-scoped, non-provider expansion sources for `${VAR}` references in
MCP/plugin env, headers, URLs, commands, and args; those values are not written
into the process environment, and Reasonix control variables such as
`REASONIX_HOME`, `REASONIX_STATE_HOME`, and `XDG_CONFIG_HOME` are ignored there.

Caches remain in the OS cache directory, for example
`~/Library/Caches/reasonix` on macOS, `$XDG_CACHE_HOME/reasonix` or
`~/.cache/reasonix` on Linux, and `%LOCALAPPDATA%\reasonix\cache` on Windows.
Set `REASONIX_CACHE_HOME` to override the cache root. When `REASONIX_HOME` is
set, the cache is placed under `$REASONIX_HOME/cache` (unless
`REASONIX_CACHE_HOME` is also set, which takes precedence).

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

If old v0.x sessions live outside the known legacy locations — for example a
Windows v0.52 install/data directory chosen during setup — pass that directory
explicitly:

```text
/migrate --from "D:\OldReasonix"
```

The explicit form imports sessions only. The path may be the old install
directory, a `.reasonix`/data directory, or the `sessions` directory itself;
Reasonix checks the common layouts below that root and uses a source-specific
marker, so a previous plain `/migrate` run does not hide the later import.

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
- Plain `/migrate` rescans the legacy locations listed above. Use
  `/migrate --from <path>` only for a known v0.x session source; it is not a
  backup restore tool or a downgrade importer.
