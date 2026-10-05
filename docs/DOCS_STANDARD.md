---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-09-17
---

# Documentation standard

Scope: every Markdown file on the `studio` branch outside `benchmarks/`.
Enforcement: `make check` and CI run `repolint`; rule IDs below name its findings.

## 1. Ownership

| ID | Rule |
| --- | --- |
| O1 | Every path MUST match a `.github/CODEOWNERS` rule listing exactly a primary and a backup. |
| O2 | The primary MUST review and merge changes to the path; the backup acts when the primary is unavailable. |
| O3 | A CODEOWNERS rule MUST match at least one file. Delete the rule with the file. |
| O4 | Owners MUST have write access to the repository. |

Current areas:

| Area | Paths | Primary | Backup |
| --- | --- | --- | --- |
| Kernel, Studio shell, tooling, standing instructions | `*` | @esengine | @SivanCola |
| User guides and references | `docs/`, `sdk/`, `README*`, `SECURITY.md` | @SivanCola | @esengine |
| Studio contracts and processes | `docs/STUDIO_*.md`, `docs/DOCS_STANDARD.md`, `docs/*_CONTRACT.md`, `docs/TOOL_APPROVAL_MODES.md`, `docs/CAPABILITY_DIAGNOSTICS.md`, `docs/CHECKPOINTS.md`, `docs/EXTENSION_*.md`, `docs/SUBAGENT_*.md`, `docs/SESSION_MEMORY_RETRIEVAL.md` | @esengine | @SivanCola |
| Release lines and moving between them | `docs/ROADMAP.md`, `docs/MIGRATING.md` | @esengine | @SivanCola |
| Kernel design proposals | `docs/design/` | @esengine | @SivanCola |
| Release notes and release signing | `release-notes/`, `.signpath/`, `release-studio.yml` | @esengine | @SivanCola |

## 2. Document classes

| Class | Paths | Header | Prose width |
| --- | --- | --- | --- |
| Document | everything not listed below | required | 320 |
| Landing | `README.md`, `README.zh-CN.md` | none (CODEOWNERS) | 320 |
| Machine-read | `REASONIX.md`, `AGENTS.md`, `CLAUDE.md`, `.github/`, `.reasonix/`, skill and guardian prompts, `docs/EXTENSION_PROTOCOL.generated.md` | none (CODEOWNERS) | 320 |
| Release note | `release-notes/` | none (published verbatim) | 200 per item |

The class table lives in `tools/repolint/docs.go`. Change both together.

## 3. Header (`doc-owner`)

A document MUST start with this block:

```yaml
---
owner: @github-handle      # CODEOWNERS primary for this path
backup: @github-handle     # CODEOWNERS backup for this path
status: active             # active | deprecated
reviewed: YYYY-MM-DD       # last time the owner confirmed it matches the code
---
```

| ID | Rule |
| --- | --- |
| H1 | `owner` and `backup` MUST equal the first and second owner of the matching CODEOWNERS rule. |
| H2 | `status: deprecated` means scheduled for deletion; link the replacement in the first line. |
| H3 | The owner SHOULD update `reviewed` whenever the described behavior changes. |

## 4. Writing (`doc-prose`)

| ID | Rule |
| --- | --- |
| W1 | State content as numbered rules, ordered steps, or tables. |
| W2 | A paragraph or list item MUST stay within 320 display columns (a CJK character counts as two). |
| W3 | Use MUST, SHOULD and MAY for requirements. Anything else is information. |
| W4 | Describe the current behavior. History and rationale belong in commit messages. |
| W5 | Put commands in fenced code blocks so they can be copied as-is. |

## 5. Types and required sections

| Type | File pattern | Required sections, in order |
| --- | --- | --- |
| Contract | `*_CONTRACT.md`, `SPEC.md` | Scope, Rules (ID table), Enforcement |
| Runbook | `*_RELEASE.md`, `*_RUNBOOK.md` | Owners, Preconditions, Steps, Verification, Recovery |
| Guide | everything else under `docs/` | Purpose, Steps or Reference tables |

Sections are reviewed by the owner. The gate checks header, width and language only.

## 6. Language (`doc-language`)

| ID | Rule |
| --- | --- |
| L1 | English is the maintained language of every document. |
| L2 | Only `README.zh-CN.md`, `docs/GUIDE.zh-CN.md` and `docs/CLI.zh-CN.md` MAY keep a Chinese copy. |
| L3 | Release notes are written in Chinese, the language Studio users read. |

## 7. Lifecycle

| ID | Rule |
| --- | --- |
| C1 | A document describing removed behavior MUST be deleted in the change that removes it. |
| C2 | Git history is the archive. Do not add `archive/` directories. |
| C3 | Existing violations are recorded in `tools/repolint/baseline.json` and may only go down. |
