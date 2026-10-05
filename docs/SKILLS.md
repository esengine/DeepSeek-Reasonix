---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-01
---

# Skills

Reasonix loads [Agent Skills](https://agentskills.io): a folder with a
`SKILL.md` file whose frontmatter names and describes the skill and whose body
is the playbook. Skills written for other agents work unchanged.

To write and verify a skill from an empty directory, follow the
[community author guide](MARKET_AUTHOR_GUIDE.md). It also shows how to package
a skill and submit a fixed version for review.

## Where skills are found

Each root below is scanned for `<name>/SKILL.md`. On a name collision the
earlier scope wins: project, then custom, then global, then built-in.

| Scope | Directories |
| --- | --- |
| Project | `<repo>/.reasonix/skills`, `<repo>/.agents/skills`, `<repo>/.agent/skills`, `<repo>/.claude/skills` |
| Custom | every entry in `[skills] paths` |
| Global | `<Reasonix home>/skills` (`~/.reasonix` on macOS/Linux, `%APPDATA%\reasonix` on Windows, or `$REASONIX_HOME`), `~/.reasonix/skills`, `~/.agents/skills`, `~/.agent/skills`, `~/.claude/skills` |
| Built-in | shipped with Reasonix (`explore`, `research`, `review`, `security-review`, …) |

Symlinked skill folders are followed. Under `.claude` roots a flat `<name>.md`
file also loads, but only when it carries skill frontmatter.

Installers that target `.reasonix/skills` — for example
`npx skills add <repo> -a reasonix` — land in a directory Reasonix already scans.

## How a skill runs

The model receives a catalog of enabled, model-listed skills: names, clipped
descriptions (about 130 characters per entry), and a subagent tag where
applicable.

Skills with `invocation: manual` stay out of the listing but remain callable
by `/<name>` and, unless model invocation is disabled, `run_skill`.
The model cannot call a skill with `disable-model-invocation: true`;
the user can still invoke it explicitly by `/<name>`.

The entry listing has a 4000-character budget. If descriptions would exceed
it, the catalog lists names without descriptions and points to
`use_capability` search; if names also exceed it, whole entries are omitted
with a count of the remaining skills, which search still reaches.

Reasonix projects the catalog into user-turn context when first needed, when
its visible contents change, or after compaction. The cache-stable system
prefix stays unchanged.

After every skill is switched off, a previously delivered listing is replaced
by a short notice. No catalog is sent when implicit invocation is disabled.

Each skill's body is loaded when that skill is invoked.

- The model invokes a skill with the `run_skill` tool.
- You invoke one by typing `/<name>` (plugin skills are `/<plugin>:<name>`).
- Markdown files in the skill's `references/` folder are appended to the body,
  and the scripts in its `scripts/` folder are listed so the model can run them.
- The result carries the absolute path of the `SKILL.md`, so any other file in
  the folder can be read from there.

## Frontmatter

`name` and `description` are the Agent Skills fields. Reasonix also reads:

| Key | Meaning |
| --- | --- |
| `allowed-tools` | Tools a subagent skill may use. |
| `runAs` | `inline` (default): the body joins the current turn. `subagent`: the skill runs in an isolated child loop and only its final answer returns. Claude-style `context: fork` or `agent:` also selects `subagent`. |
| `model`, `effort` | Model and reasoning effort for a subagent skill. |
| `read-only` | Run a subagent skill with writer tools removed and read-only shell. |
| `invocation` | `manual` keeps the skill out of the model's listing; it stays callable by name unless model invocation is disabled. |
| `disable-model-invocation` | `true` prevents model calls; the user can still invoke the skill explicitly. |
| `requires` | Ready capabilities required for model invocation, e.g. `mcp-server:github`. See the MCP requirements example below. |

Unknown keys are ignored, so a skill written for another agent loads as-is.

## Managing skills

- `/skills` lists every loaded skill with its scope and path.
- `/skills disable <name>` and `/skills enable <name>` hide or restore a skill in
  this project; add `--global` to apply everywhere.
- `reasonix doctor` reports skill health warnings, such as a missing
  description or a required capability that is not available.

```toml
[skills]
paths = ["~/my-skills", "../shared/skills"]   # extra roots
excluded_paths = ["~/.agents/skills"]         # skip a convention root
disabled_skills = ["review"]                  # hidden until /skills enable
```

Skills also arrive inside plugin packages; see [PLUGIN_PACKAGES.md](PLUGIN_PACKAGES.md).

## Declaring MCP requirements

1. Use the configured server name and its actual tool names. For a server named
   `notes` exposing `read`, a skill can declare both requirements:

   ```markdown
   ---
   name: notes-check
   description: Check notes using the configured MCP reader
   requires: mcp-server:notes, mcp-tool:notes/read
   ---
   Read the notes available from the configured notes server.
   Report the relevant entries and identify anything that could not be verified.
   ```

2. Confirm IDs in the session's capability catalog: ask the model to inspect
   `mcp-server:notes` with `use_capability`, then copy the returned IDs.
   The reader must already be configured; `requires` does not install it,
   supply credentials, or grant permission.

3. Model calls through `run_skill`, `read_skill`, `use_capability` and
   `slash_command` require every declared capability to be `ready`. Missing
   tools, disabled servers and cached schemas without a live connection do not
   qualify. Correct IDs or connect the enabled server in Studio, then retry.

4. An enabled server with no usable schema cache may connect at startup to
   discover its tools. Once schemas are cached, a deferred server can remain
   disconnected until needed; its cached tools alone do not make a required
   capability `ready`.

5. A user explicitly typing `/notes-check` can still load the playbook when its
   dependencies are unavailable, for example to ask about setup. That does not
   connect a disabled server or bypass the tool's own execution checks.
   Write the body so it explains missing setup rather than claiming a result.

6. Use [capability diagnostics](CAPABILITY_DIAGNOSTICS.md) to check configuration;
   its live probe is separate from an open session's connection. `reasonix doctor`
   warns about missing or host-failed MCP servers for `auto-use: require` skills.
   A clean report does not prove runtime readiness.

## Sharing a skill with a repository

Commit a project skill so collaborators receive the same playbook with the
repository. From the repository root, create a small review checklist:

```bash
mkdir -p .agents/skills/team-review
cat > .agents/skills/team-review/SKILL.md <<'EOF'
---
name: team-review
description: Review local changes using the team checklist
---
Read the current diff and the files it changes.
Report correctness issues with a file path, a concrete trigger, and the expected behavior.
Separate findings from verification that still needs to run.
EOF
git add .agents/skills/team-review/SKILL.md
git commit -m "Add shared review skill"
```

Check that the file is tracked, including any required files in its skill
folder. Open Reasonix in the checkout, confirm `/skills` lists `team-review`
from the project path, and invoke `/team-review inspect this change`.
A project skill takes precedence over a personal skill with the same name.

`/skills disable team-review` records a personal project switch in your
Reasonix home; it does not edit the committed playbook. Linked worktrees of
the same repository share that project switch within one Reasonix home.
Collaborators using separate homes can choose independently.

Use `/skills enable team-review` to restore it. To retire the shared playbook,
remove its tracked folder through the repository's normal review process.
