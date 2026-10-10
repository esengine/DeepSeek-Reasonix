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
| `paths` | File globs that gate when the model is shown the skill. See [Activating a skill by file](#activating-a-skill-by-file). |

Unknown legacy keys are ignored. The `delivery` and `authority` namespaces
have explicit validation rules:

1. A subagent profile MAY declare the typed verdict it owes with
   `delivery.review-report`. The accepted scalar values are `review` and
   `security`, case-insensitive:

   ```yaml
   delivery:
     review-report: review
   ```

2. `delivery` MUST be a mapping with only recognized fields. An unknown field,
   a non-scalar `review-report`, or an unsupported verdict rejects the skill
   during loading. Correct the declaration before trying to invoke it.
3. Authors MUST NOT declare `authority`, including an empty block. The host
   owns the authority to satisfy a review obligation; declaring a delivery
   requirement does not grant it. A skill declaring `authority` is rejected.
4. A top-level `review-report` is diagnosed but does not set the delivery
   requirement. Move it under `delivery` as shown above.

## Activating a skill by file

A skill that declares `paths` stays out of everything the model reads until the
session has touched a file one of its globs matches; from then on it is listed
for the rest of the session. A skill without `paths` is unaffected.

```yaml
---
name: go-style
description: House style for Go code
paths:
  - "internal/**/*.go"
  - "*.go"
---
```

Either a YAML list or a comma-separated string works (`paths: "*.go, docs/**"`).

What counts as touching a file:

- A completed tool call that names the file through the tool's own declaration:
  a read, or a write, edit, move or delete by a built-in file tool, or `grep`
  pointed at one file. A zero-match `grep` of a named file still counts.
- The host observes this. Nothing is inferred from shell commands or from the
  model's text, so a file reached only through `bash` does not count.
- A refused, hook-blocked or failed call does not count, and neither does a
  directory or a file outside the workspace.

How globs match:

- They are rooted at the workspace. `*.go` matches only `main.go`, never
  `pkg/main.go`; write `**/*.go` for any depth. `src/**/*.go` is anchored at `src`.
- A trailing slash (`docs/`) means everything under that directory. A leading
  `./` or `/` is dropped. Paths use `/` on every platform.
- Braces expand (`*.{ts,tsx}`). One `paths` list may expand to at most 1,000
  patterns in total, and a glob over 512 bytes is refused.
- Negation (`!x`), `..` and malformed globs are refused. `doctor` reports them,
  and a skill whose globs were all refused stays hidden rather than always-on.
- Matching is case sensitive on every platform. On macOS and Windows, where
  `README.md` and `readme.md` are one file, a model that spells an existing file
  with the wrong case misses the match and the skill is not shown.

Where the skill is hidden, and where it is not:

- Hidden until a match: the listing sent with the turn, `use_capability`
  search, list and inspect, the `slash_command` listing, and the names offered
  after a wrong `run_skill`.
- The `slash_command` listing follows each skill's `paths` as registered, so an
  edit to `paths` made mid-session reaches it only after a command reload; the
  per-turn listing and `use_capability` read the files live.
- Never gated: `/<name>` from the user, and `run_skill` with the exact name.
  Only discovery waits for a file.

When the listing changes:

- The listing is judged afresh each turn from the set of touched files, so the
  turn after a match carries the skill.
- A resumed conversation rebuilds the set from its transcript. Switching to
  another conversation starts the set over.
- A file a sub-agent touches does not count for the session that delegated to
  it. Delegating a file-heavy job therefore does not make a path-gated skill
  appear there; touch the file in that session, or invoke the skill with `/<name>`.

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
