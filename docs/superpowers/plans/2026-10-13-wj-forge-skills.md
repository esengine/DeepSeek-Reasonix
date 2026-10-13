# WJ Forge Built-in Skills Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `executing-plans` after the user approves this plan and selects native execution. Work one task at a time and keep the changes reviewable.

**Goal:** Add WJ Forge's 14 Skill definitions and seven source-validated update paths to Reasonix's native Skill catalog.

**Architecture:** Compile the Skill documents into Reasonix's existing `builtincontent` filesystem and load them through the existing Skill Store. Take the 14-entry catalog metadata from `E:\home\wj-forge\src-tauri\src\skills.rs` and the seven updater URLs/name mappings from `E:\home\wj-forge\src-tauri\src\skill_updates.rs`. Keep stable IDs and user disable preferences; store explicitly downloaded versions separately from source code and below project/custom/global user-authored Skill overrides. Expose category, source, and update status through the existing Skills settings surface.

**Tech Stack:** Go, `embed.FS`, Reasonix Skill Store/frontmatter parser, existing desktop Go bindings and React/TypeScript settings panel.

**Spec:** `docs/superpowers/specs/2026-10-13-wj-forge-skills-cognee-design.md`

## Global Constraints

- Ship the 14 stable Skill IDs listed in the spec, enabled by default and disabled through the existing `disabled_skills` preference.
- Only the seven fixed Skill sources in the spec can be checked or updated; source IDs, URLs and expected upstream names are compiled into the app.
- Updates require explicit user action, HTTPS-only redirects, bounded response size, exact upstream frontmatter-name validation and atomic replacement.
- Skill text supplies workflow guidance only; it cannot change tools, `AllowedTools`, runtime bindings, permission policy or file access.
- Project/custom/global same-name Skills continue to take precedence over built-in Skill content.
- Keep provider-visible tool names and schemas unchanged.
- Do not add or run tests unless the user explicitly asks for tests or implementation verification.

## Review Focus

- Conflicting upstream and Reasonix names (`cognee` → `cognee-memory`, `design-taste-frontend` → `taste-skill`) must validate upstream identity then keep the stable local ID.
- Redirects must remain HTTPS, and an oversized/truncated response must leave the previous installed version intact.
- Disabled Skills must stay disabled after an update and across an app upgrade.
- A downloaded Skill must not shadow a project/custom/global user-authored Skill with the same stable name.
- Unsupported catalog entries must show no update path even if they carry a source URL.

---

### Task 1: Embed WJ Forge's Skill catalog

**Files:**
- Create: `internal/skill/builtincontent/wj-forge/*/SKILL.md` (14 stable directories)
- Modify: `internal/skill/builtincontent/embed.go`
- Modify: `internal/skill/builtin_embed.go`
- Modify: `internal/skill/skill.go`
- Modify: `desktop/app.go` (`SkillView` and `SkillsSettings` projection)
- Modify: `desktop/frontend/src/components/CapabilitiesPanel.tsx`

**Produces:** The existing `skill.Store` discovers the embedded entries as `ScopeBuiltin`; metadata carries `Category` and `SourceURL`; the Skills panel can display category/source while using existing enable/disable actions.

- [ ] Add one `SKILL.md` per stable ID from the spec. Set frontmatter `name`, `description`, `category`, and `source-url`; put WJ Forge's shipped workflow description and the no-extra-authority rule in the body. The `officecli` content must say its adapter is unavailable unless the host actually provides it.
- [ ] Generalize `builtincontent.embed.go` from the single `reasonix-guide` directory to the package's embedded Skill tree while retaining on-demand body loading and deterministic directory traversal.
- [ ] Add `Category` and `SourceURL` to `skill.Skill`; parse them from embedded Skill frontmatter in `builtincontent.ParseSkillMarkdown` and copy them in `skillFromEmbedded`.
- [ ] Add category/source fields to desktop `SkillView` projection and render them in the existing Skills settings panel without changing its toggle semantics.
- [ ] Format the changed Go files and run `go build ./...`; inspect the built-in list and settings projection for all 14 stable IDs and unchanged provider schemas.

### Task 2: Add source-validated Skill updates

**Files:**
- Create: `internal/skill/builtin_updates.go`
- Modify: `internal/skill/builtin_embed.go`
- Modify: `internal/skill/skill.go` (updated built-in content precedence)
- Modify: `internal/control/controller.go` or a focused `internal/control/skill_updates.go`
- Modify: `desktop/app.go`
- Modify: `desktop/frontend/src/components/CapabilitiesPanel.tsx`

**Consumes:** Task 1's stable Skill metadata and `ScopeBuiltin` entries.

**Produces:** A fixed-source updater with check/list/update operations. Updated built-in bodies live under the Reasonix app data directory, are loaded as built-in update versions, and remain below project/custom/global user-authored Skills in precedence.

- [ ] Define a compiled `SkillUpdateSource` table for exactly `cognee-memory`, `ponytail`, `taste-skill`, `zerotoken-skill`, `weiliaozi-skill`, `moon-lovers`, and `officecli`, with fixed HTTPS URL and expected upstream frontmatter `name`.
- [ ] Implement check and update operations with a 15-second request timeout and 256 KiB response limit, HTTPS redirect validation, ETag version reporting, frontmatter validation, stable-ID normalization, and atomic file replacement. Check must not install content.
- [ ] Load a saved update body over the embedded body for its stable ID while keeping its scope at built-in priority; never modify user enable preferences or security-bearing frontmatter fields.
- [ ] Expose supported/unsupported state, installed/latest version, check time, and per-source update errors through controller/desktop methods; do not include response bodies or credentials in status.
- [ ] Add explicit Check and Update actions to the existing Skills settings view. Require a fresh check before applying an update; report failures while retaining the previous body.
- [ ] Format Go files, run `go build ./...` and the desktop frontend production build; manually inspect the source table, update-state DTO, and generated settings action flow against the five Review Focus cases.

---

### Task 3: Document the embedded catalog and update behavior

**Files:**
- Modify: `docs/EXTENSIONS.zh-CN.md`

**Consumes:** Tasks 1–2's public behavior.

**Produces:** User-facing instructions listing the stable IDs, update behavior, and the distinction between a Skill guide and an available tool.

- [ ] Document where users enable/disable the built-ins and how they check/apply updates.
- [ ] State that only the seven fixed sources update, Skill Markdown does not grant tools, and OfficeCLI execution is not supplied by the `officecli` guide.
- [ ] Check links and commands against the desktop/controller names introduced in Tasks 1–2.
