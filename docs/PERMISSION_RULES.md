# Permission rules

The `[permissions]` section in `reasonix.toml` (project) or `config.toml` (user) decides, per tool call, whether Reasonix runs the tool, asks you first, or blocks it. This page documents the exact rule syntax so rules are not guesswork.

See [TOOL_APPROVAL_MODES](./TOOL_APPROVAL_MODES.md) for how the desktop Ask / Auto / Yolo control relates to the configured policy.

## Section shape

```toml
[permissions]
mode  = "ask"        # ask | allow | deny — the writer fallback
allow = ["Edit(src/**)", "Bash(go test:*)"]
ask   = ["Bash(git push:*)"]
deny  = ["Bash(rm -rf*)"]
```

- `mode` is the fallback decision for writer tools when no rule matches (`ask` is the default). Read-only tools always fall back to **allow**, never `mode`.
- `allow`, `ask`, `deny` are rule lists. A call's decision is resolved with strict precedence:

```
deny  > ask  > allow  > fallback
```

  So a call that matches a deny rule is blocked even if an allow rule also matches.

## Rule forms

| Form | Meaning |
| --- | --- |
| `ToolName` | Matches every call to that tool. |
| `ToolName(specifier)` | Matches calls whose subject matches the specifier (glob or bash prefix). |
| `ToolName=literal` | Legacy exact form: matches the subject verbatim, no globbing. Kept so old configs keep working. |

Surrounding whitespace is trimmed. A rule with an empty tool name is invalid.

### Tool names vs tool IDs

Rules use the friendly name `Bash` for the `bash` tool and `Edit` for the **file-mutation group** — `write_file`, `edit_file`, `multi_edit`, `move_file`, `notebook_edit`, `delete_range`, `delete_symbol`. A bare `Edit` rule therefore covers all seven file writers at once.

Every other name is the literal tool ID, including the remaining built-in tools (`read_file`, `grep`, `glob`, `ls`, `bash_output`, `wait`, …), MCP server tools, and session tools. Tool IDs are lowercase and match the `Tool` column in [TOOL_CONTRACT](./TOOL_CONTRACT.md).

### Specifier grammar

The specifier is matched against the call's **subject** — the string that identifies what the tool touches:

- `bash` → the command line.
- file tools (`Edit` group, `read_file`) → the file path.
- `move_file` → both `source_path` and `destination_path`; the call is allowed only if every path is allowed.
- `grep` / `glob` → the search pattern.
- `ls` → the directory path.

Globs use two wildcards:

- `*` matches any run of characters, **including `/` and `\`** — so `src/**` matches files at any depth under `src/`.
- `?` matches exactly one character.

The pattern must match the whole subject (it is not a substring search). Shell bracket expressions are not special here.

**Bash prefix rules.** A specifier ending in `:*` (e.g. `Bash(go test:*)`) is a command-prefix rule: it matches any command whose first words equal the prefix at a word boundary, so `go test ./...` and `go test -v ./...` both match `Bash(go test:*)`. Compound commands are matched segment by segment, so `git add . && git commit && git push` is covered by `Bash(git push:*)`. The legacy `Bash(cmd *)` (space-star) form is also accepted.

**Absolute vs relative paths.** Specifiers match the raw path string the model passes. Relative workspace globs such as `Edit(src/**)` are the robust choice. Absolute paths (`Edit(/etc/*)`, `Edit(C:\...)`) and `..` escapes are accepted but flagged with a warning, because a rule that reaches outside the workspace usually defeats the "allow everything inside, lock everything outside" intent.

### What is validated

Structural mistakes are **rejected when a rule is saved** (Settings → Permissions, or a config write): unparseable rules, empty specifiers (`Edit()`, `Bash=`), and unbalanced specifiers (`Edit(src`).

Warnings are surfaced in **diagnostics** — `reasonix doctor capabilities`, or Settings → Diagnostics in the desktop app:

- A tool name that is not a built-in tool (MCP and session tool names are valid, so this is a spelling check, not an error).
- A specifier that is an absolute or workspace-escaping path.
- A specifier with leading or trailing whitespace, which can never match a real call (`Edit(src/** )` matches the literal path `src/** `).
- Structural mistakes that already exist in a config file.

The Diagnostics page also lists the effective decision for each built-in tool on a bare call — which rule fired (`allow` / `ask` / `deny`) or the fallback — so you can see that a rule actually took effect.
