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
| Global credentials file fallback | `<Reasonix home>/credentials` |
| Global slash commands | `<Reasonix home>/commands/` |
| Global skills | `<Reasonix home>/skills/` |
| Global hooks | `<Reasonix home>/settings.json` |
| Hook trust store | `<Reasonix home>/trust.json` |
| Sessions | `<Reasonix home>/sessions/` |
| Archives | `<Reasonix home>/archive/` |
| Memory | `<Reasonix home>/memory/` and `<Reasonix home>/projects/` |

Global credentials use `credentials_store = "auto"` by default. In auto mode,
Reasonix tries the OS credential store first and falls back to
`<Reasonix home>/credentials` when the keyring is unavailable. Set
`credentials_store = "keyring"` to require the OS credential store, or
`credentials_store = "file"` to always use the file fallback. `REASONIX_CREDENTIALS_STORE`
can override the mode for CI, tests, or portable installs.

New API keys saved by the setup wizard, the desktop app, or an interactive
missing-key prompt are written to the configured credential store above. Project
`.env` files are still loaded first for compatibility and deliberate per-project
overrides, but Reasonix does not write new keys into a project `.env`.

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

Legacy credentials, memory files, and sessions are also imported into the
configured credential store / Reasonix home when the new destination does not
already exist. If the new global config already exists, it wins and legacy
config files are only kept as compatibility fallbacks.

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
