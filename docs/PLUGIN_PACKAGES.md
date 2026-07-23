# Reasonix Plugin Packages

Reasonix plugin packages bundle skills, hooks, and MCP servers behind one
installable unit.

## CLI Mode

Use `reasonix plugin` when installing or managing plugin packages from a
terminal. Plugin packages are installed globally under the Reasonix home
directory.

### Install From CLI

`install` accepts one source:

- A GitHub repository, such as `git:github.com/obra/superpowers` or
  `https://github.com/obra/superpowers`.
- A GitHub branch or subdirectory URL, such as
  `https://github.com/owner/repo/tree/main/path/to/plugin`.
- A local directory that contains `reasonix-plugin.json`,
  `.codex-plugin/plugin.json`, or `.claude-plugin/plugin.json`.

Preview the install plan without writing files:

```bash
reasonix plugin install git:github.com/obra/superpowers --dry-run
```

Install a plugin after reviewing the plan:

```bash
reasonix plugin install git:github.com/obra/superpowers --yes
```

Install with an explicit name or replace an installed plugin with the same name:

```bash
reasonix plugin install git:github.com/obra/superpowers --name superpowers --replace --yes
```

Use a local directory in developer mode:

```bash
reasonix plugin install /path/to/plugin --link --replace --yes
```

CLI install flags:

- `--dry-run` plans and validates the install without writing files.
- `--yes` is required for any install that writes files.
- `--replace` allows the source to replace an installed plugin with the same
  name.
- `--name <name>` or `--name=<name>` overrides the name from the plugin
  manifest for this install.
- `--link` links a local plugin directory instead of copying it into Reasonix's
  plugin storage. Moving or deleting that directory breaks the linked plugin.

Running `reasonix plugin install <source>` without `--dry-run` or `--yes`
refuses to write files and prints a reminder to rerun with one of those flags.
Install and remove commands print the structured JSON response from the same
install-source backend used by the desktop UI.

Installed plugin state is stored in:

```text
~/.reasonix/plugin-packages.json
~/.reasonix/plugins/<name>/
```

### Manage From CLI

List installed plugins:

```bash
reasonix plugin list
```

Show one plugin's metadata, root, source, and exported capability counts:

```bash
reasonix plugin show superpowers
```

`show` also prints the concrete capability inventory when available:

- **skills** include suggested `/<plugin>:<skill>` invocations and descriptions.
- **commands** include `/<plugin>:<command>` invocations, argument hints, and
  descriptions.
- **hooks** list lifecycle events, matchers, and commands or context files.
- **mcpServers** list server names, transports, and launch targets.

Check that the manifest and skill roots are readable:

```bash
reasonix plugin doctor superpowers
```

For a workspace-wide capability report (skills, hooks, MCP merge, package roots), see
[Capability diagnostics](./CAPABILITY_DIAGNOSTICS.md):

```bash
reasonix doctor capabilities --json
# Desktop: Settings → Diagnostics
# Agent:   /reasonix-guide
```

Enable or disable a plugin without uninstalling it:

```bash
reasonix plugin disable superpowers
reasonix plugin enable superpowers
```

Remove a plugin:

```bash
reasonix plugin remove superpowers --yes
```

`remove` also accepts `uninstall` as an alias. It requires `--yes` because it
writes state and removes copied plugin content. For linked local plugins, the
external source directory is left in place.

### Use Installed Plugins From CLI

Installed plugins do not open a separate chat surface. When a plugin is enabled,
Reasonix loads its capabilities into normal interactive sessions:

- Run `/plugins` inside an interactive session to list installed plugin
  packages. Run `/plugins show <name>` to inspect a plugin's exported skills,
  hooks, MCP servers, and usage hints without leaving the chat.
- **Skills** appear in `/skills`. Invoke a plugin skill with
  `/<plugin>:<skill> [args]`, or ask
  naturally and let the agent choose a matching skill by description.
- **Hooks** run automatically at their configured lifecycle events, such as
  `SessionStart`, `UserPromptSubmit`, `PreToolUse`, or `PostToolUse`.
- **MCP servers** join the normal MCP/tool flow. Ask for the task you want done;
  Reasonix can call the plugin's tools when they are relevant.

After installing, enabling, disabling, or updating a plugin from a separate
terminal while a session is already running, start a new `reasonix` session or
reopen `/skills` to verify the current session sees the expected skills.

## Desktop Settings

Open **Settings -> Plugins** to install and manage plugin packages without using
the CLI.

### Install Plugins

The installer has two modes:

- **Local folder**: click **Choose plugin folder** and select a plugin directory
  on disk. The selected path is shown next to the button.
- **Git repository**: enter a Git source such as
  `git:github.com/obra/superpowers`. **Install name (optional)** can override
  the plugin manifest name for this install or overwrite.

Use the action buttons after choosing the source and options:

- **Preview** validates the source and shows the planned install actions without
  writing files.
- **Install plugin** installs the selected source using the current options.
- **Refresh plugins** reloads the installed-plugin list from disk and config.

Installer options:

- **Overwrite same-name plugin** allows the current source to replace an
  installed plugin with the same name. Leave it off when duplicate-name installs
  should fail instead of replacing existing content.
- **Developer mode: link source folder** appears for **Local folder** installs.
  It links the selected directory instead of copying it into Reasonix's plugin
  storage. Use it while developing or debugging a plugin. Moving or deleting the
  selected directory will break the linked plugin.

Preview is the safest first step for a new Git source or local plugin directory.

### Manage Installed Plugins

The installed-plugin list shows each plugin package and its exported skills,
hooks, and MCP servers. Use **Refresh plugins** after editing plugin files or
changing config outside the app.

Expand a plugin row to manage it:

- Enable or disable the plugin.
- Read **How to use** for the plugin's exported skills, hooks, and MCP servers.
- **Update** pulls or refreshes an installed plugin when an update source is
  available.
- **Doctor** checks the plugin manifest and reports warnings or diagnostics.
- **Remove plugin** uninstalls the package after confirmation.

### Use Installed Plugins From Desktop

The desktop settings page uses the same runtime model as the CLI:

- Expand an installed plugin to see its **How to use** section.
- In any desktop session, type `/plugins` to list installed plugins, or
  `/plugins show <name>` to see the same usage details from the chat surface.
- Skills are shown with package-qualified direct commands such as
  `/superpowers:writing-plans`; they are also discoverable from `/skills` in a
  session.
- Plugin commands are shown and invoked with package-qualified names such as
  `/superpowers:plan`.
- Hooks and MCP servers are listed for transparency. They do not need a manual
  "run" button: enabled hooks trigger automatically, and MCP tools are available
  through ordinary tool use.
- If a currently open session does not reflect a plugin change, refresh the
  plugin list and open a new session.

## Native Manifest

Reasonix plugins can declare `reasonix-plugin.json` at the plugin root:

```json
{
  "name": "example",
  "version": "1.0.0",
  "description": "Example plugin",
  "skills": "skills",
  "hooks": {
    "SessionStart": [
      {
        "command": "hooks/session-start",
        "description": "Load startup context"
      }
    ]
  },
  "mcpServers": {
    "helper": {
      "command": "bin/helper"
    }
  }
}
```

Relative paths are resolved inside the plugin root. Reasonix does not run
third-party install scripts during plugin installation.

## Codex & Claude Compatibility

Reasonix also reads Codex plugin manifests at `.codex-plugin/plugin.json` and
Claude plugin manifests at `.claude-plugin/plugin.json`. The install preview
reports `full`, `partial`, or `none` compatibility, lists mapped capabilities,
and identifies every skipped entry. A non-native package with no mapped
capabilities is blocked instead of being recorded as an unusable installation.
`full` means every declared capability in the manifest parsed and mapped to a
Reasonix construct; it does not by itself guarantee every runtime decision an
imported hook can make is honored. `PreToolUse`/`PermissionRequest` "deny" and
`PermissionRequest` "allow" are implemented, but a hook's `updatedInput` or
`PreToolUse`'s `ask`/`defer` decisions are chosen by the script's stdout at
call time, not by anything in the manifest, so they can't be flagged during
install; see the hook bullet below for what's implemented.
GitHub-hosted multi-plugin marketplaces with a
`.claude-plugin/marketplace.json` can be installed from the repository root
when their plugin entries use relative string sources such as
`./plugins/example` or `plugins/example`; preview shows one action per plugin
before anything is written. Set the optional install name to a marketplace
plugin name to select only that entry. Object sources are accepted only for a
GitHub repository URL pinned to a full commit SHA. Unpinned external strings,
npm, `strict: false`, and other advanced marketplace protocols are skipped in
a bulk install and rejected when selected by name. For packages
such as Superpowers and Claude-style skill packs, Reasonix maps:

- `skills` to Reasonix skill roots. A Claude manifest that declares no
  `skills` field falls back to the conventional `skills/` (or `.claude/skills/`)
  directory, matching Claude's own auto-discovery. Plugin skills are displayed
  and invoked canonically as `/<plugin>:<skill>`. An unambiguous `/<skill>` is
  still accepted as a hidden compatibility alias; project and user skills keep
  their short names, while same-name skills from multiple plugins remain
  independently addressable only by their qualified names. This user-facing
  namespace does not change the bare skill identifiers in the model skill index
  or the `run_skill` tool.
- `commands/` (and `.claude/commands/`) to Reasonix custom slash commands: each
  `<name>.md` prompt template is displayed and invoked canonically as
  `/<plugin>:<name>`, with frontmatter `description` / `argument-hint` and
  `$ARGUMENTS` / `$1..$N` substitution honored. An unambiguous `/<name>` remains
  accepted as a hidden compatibility alias, but it is omitted from completion,
  help, desktop menus, ACP command discovery, and the model-visible command
  list. User- and project-authored commands own their short names, and no short
  alias is created when multiple plugins export the same command name. An
  explicit custom command can also occupy the qualified name; desktop plugin
  details report that conflict. Native `reasonix-plugin.json` manifests can
  declare the same thing explicitly with a `"commands"` path list.
- `agents/*.md` to manually invoked, plugin-owned subagent profiles. Claude
  model aliases inherit the active Reasonix model; inline `tools` lists map to
  Reasonix tool names, including wildcard MCP names such as `mcp__*__search`.
  Agents use `/<plugin>:agent:<name>`, so an upstream agent and skill may share
  the same name without shadowing one another.
- `hooks/session-start-codex` to the Reasonix `SessionStart` hook when present.
- A plugin-root `CLAUDE.md` file to a built-in `SessionStart` context hook. The
  file is read directly by Reasonix, without spawning a shell command.
- `.claude/settings.json` and `hooks/hooks.json` command hooks to Reasonix hook
  events when the event names match. `matcher`, `args`, `async`, `env`, and
  timeout are preserved. `matcher` and the `tool_name` a hook script sees are
  translated between Reasonix's own tool names and Claude's (`bash` ↔
  `Bash`, `write_file` ↔ `Write`, ...), so a matcher like `"Bash"` fires
  correctly; every Reasonix subagent-spawning tool (`task`, `read_only_task`,
  `parallel_tasks`, and the dedicated `explore`/`research`/`review`/
  `security_review` wrappers) maps to Claude's single `Agent` tool, and a
  matcher can still use the legacy `Task` name. Every mapped `Agent` payload
  includes Claude's required `prompt` and `description`; Reasonix supplies a
  stable operation label when its tool call omitted the optional description.
  `tool_input` keys that
  Reasonix names differently from Claude are renamed too — `path` becomes
  `file_path` for `Read`/`Write`/`Edit`/`MultiEdit` and `notebook_path` for
  `NotebookEdit`, `name`/`arguments` become `skill`/`args` for `Skill`,
  `job_id` becomes `task_id` for the current `TaskOutput`/`TaskStop`, the
  dedicated subagent wrappers' `task` becomes `Agent`'s `prompt`, and
  `parallel_tasks` synthesizes `Agent`'s `prompt` from its sub-task prompts
  (keeping `tasks` alongside) — so a guard reading `.tool_input.file_path`
  or `.tool_input.prompt` sees the target instead of failing open on an
  empty value. Legacy `BashOutput`/`KillShell` matchers still fire while the
  emitted names and fields use current Claude vocabulary. `bash_output`
  supplies `TaskOutput`'s required non-blocking fields; `wait` also maps to
  `TaskOutput`, including `task_id` when it waits for exactly one job, and
  omits `TaskOutput`'s optional `timeout` for an unbounded wait rather than
  claiming a `0`ms budget.
  `AskUserQuestion` supplies omitted `multiSelect:false` and empty option
  descriptions, while `TodoWrite` derives an omitted `activeForm` from the
  task content. `NotebookEdit` also supplies `new_source` from Reasonix's
  accepted aliases, or an empty string for delete/empty-cell operations.
  Relative `file_path`/`notebook_path` values are resolved
  absolute against the payload `cwd`, matching Claude's file-tool contract,
  so prefix-matching guards inspect the path the tool actually accesses. A
  `Bash` `tool_response` is delivered in Claude's `{stdout, stderr,
  interrupted}` shape (Reasonix combines both streams into `stdout`; the
  failure error text becomes `stderr`), which the official security-guidance
  plugin's commit/push checks read; other tools' responses pass through as
  the raw result. Imported hooks receive Claude-compatible snake_case stdin
  payloads, including `hook_event_name`. Before process launch, the host
  expands `${CLAUDE_PLUGIN_ROOT}` and `${REASONIX_PLUGIN_ROOT}` (plus their
  unbraced `$NAME` and Windows `%NAME%` spellings), so plugin-relative paths
  do not depend on the target shell's environment-variable syntax. On Windows,
  a bare `sh -c` or `bash -c` hook is routed through a discovered Git for
  Windows Bash even when it is not on `cmd.exe`'s `PATH`; an explicit
  interpreter path remains untouched. If no usable Bash is installed, the hook
  reports a clear prerequisite error instead of the localized `sh is not
  recognized` output. Captured legacy-code-page output is normalized to UTF-8
  before it reaches the UI. A `PreToolUse` or
  `UserPromptSubmit` hook can still deny via exit code 2 or its JSON deny
  shape on exit 0 (`hookSpecificOutput.permissionDecision` for `PreToolUse`,
  top-level `decision:"block"` for `UserPromptSubmit`); an imported
  `PermissionRequest` hook additionally answers the permission dialog itself
  (deny or auto-allow, rather than only notifying) via exit code 2 or
  `hookSpecificOutput.decision.behavior`, matching Claude's own contract.
  `updatedInput` is not yet applied to the tool call, and a hook's `if`
  condition or `asyncRewake` field is not evaluated. A package reports partial
  compatibility with a structured warning when it declares either field, a
  `Stop`/`SubagentStop` hook (which cannot block the turn in Reasonix), or a
  matcher that covers one of three inputs Reasonix cannot losslessly express:
  `WebFetch.prompt`, `NotebookEdit.cell_id` for a Reasonix `cell_number` call,
  or `TaskOutput.task_id` when Reasonix `wait` covers multiple/all jobs. Each
  structural gap is reported once per hooks file, so a wildcard-matcher
  plugin sees one warning per gap instead of one per hook.
- A plugin-root `.mcp.json` to installed MCP entries. Claude `local` maps to
  stdio, non-ASCII display names receive stable internal IDs, and duplicate
  declarations are deduplicated. Imported servers default to
  `auto_start=false`; users connect them on demand so startup does not change
  the provider-visible tool schema.

Unsupported Claude hook item types are skipped with a warning. Reasonix does not
run third-party install scripts.

Plugin hooks receive these environment variables:

- `REASONIX_PLUGIN_ROOT`
- `REASONIX_PLUGIN_NAME`
- `REASONIX_PLUGIN_VERSION`
- `REASONIX_HOME`
- `REASONIX_WORKSPACE_ROOT`
- `CLAUDE_PROJECT_DIR`
- `CLAUDE_PLUGIN_ROOT`

## Desktop Backend Methods

Desktop exposes plugin package operations through Wails methods:

- `Plugins`
- `PlanPluginInstall`
- `InstallPlugin`
- `RemovePlugin`
- `SetPluginEnabled`
- `UpdatePlugin`
- `PluginDoctor`
