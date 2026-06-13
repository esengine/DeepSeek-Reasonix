---
description: Initialize Comet workflow with all dependencies (OpenSpec + Superpowers + Comet) in the current project
argument-hint: "[--force]"
---
Initialize the complete Comet five-phase development workflow into the current Reasonix project. This handles ALL required dependencies automatically.

## Phase 0: Check prerequisites

1. Verify Node.js ≥ 20 is available (`node --version`). If missing, tell the user to install it.
2. Verify git is available.

## Phase 1: Install OpenSpec

OpenSpec is the spec-lifecycle engine (WHAT — proposal, spec, archive).

1. Check if `openspec` CLI exists: run `openspec --version`.
2. If missing: `npm install -g @fission-ai/openspec@latest`
3. Initialize OpenSpec in the project (this writes OpenSpec skills to the convention dirs):
   ```bash
   openspec init . --tools reasonix --profile custom
   ```
4. If `openspec init` doesn't support `reasonix` as a tool ID, use `claude` instead — the skills are the same format and Reasonix scans `.claude/skills/`.

## Phase 2: Install Superpowers

Superpowers is the development methodology engine (HOW — brainstorming, planning, TDD, subagent execution).

1. Clone the Superpowers skill files from GitHub into a temp directory:
   ```bash
   git clone --depth 1 https://github.com/obra/superpowers.git /tmp/superpowers-skills
   ```
2. Copy ALL skill directories from `/tmp/superpowers-skills/skills/` into `.reasonix/skills/`:
   ```bash
   cp -r /tmp/superpowers-skills/skills/* .reasonix/skills/
   ```
3. Clean up: `rm -rf /tmp/superpowers-skills`

## Phase 3: Install Comet (already embedded in Reasonix)

Comet skills are embedded in the Reasonix installation. Copy them into the project:

```bash
# Comet skills are at <reasonix-install>/skills/comet*/
# For development, they're at .reasonix/skills/comet*/
# Ensure they exist; if missing, fetch from:
#   git clone --depth 1 https://github.com/rpamis/comet.git /tmp/comet-skills
#   cp -r /tmp/comet-skills/assets/skills/comet* .reasonix/skills/
#   chmod +x .reasonix/skills/comet/scripts/*.sh
```

## Phase 4: Create working directories

```bash
mkdir -p docs/superpowers/specs
mkdir -p docs/superpowers/plans
mkdir -p .comet
```

## Phase 5: Write .comet/config.yaml

Create `.comet/config.yaml`:
```yaml
context_compression: off
auto_transition: true
```

## Phase 6: Verify

1. Confirm all three skill groups are discoverable:
   ```bash
   # OpenSpec skills (openspec-*):
   ls .reasonix/skills/openspec-*/SKILL.md
   # Superpowers skills (brainstorming, writing-plans, etc.):
   ls .reasonix/skills/brainstorming/SKILL.md
   # Comet skills:
   ls .reasonix/skills/comet/SKILL.md
   ```
2. Confirm `openspec --version` works.
3. Confirm bash scripts are executable: `test -x .reasonix/skills/comet/scripts/comet-guard.sh`

After successful verification, tell the user: "Comet workflow is ready. Start a new change with `/comet <description>` or resume with `/comet`."
