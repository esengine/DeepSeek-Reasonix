/** Project scope wins over global. Only names+descriptions enter the prefix; bodies load lazily into the append-only log. */

import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { homedir } from "node:os";
import { join, resolve } from "node:path";
import { NEGATIVE_CLAIM_RULE, TUI_FORMATTING_RULES } from "./prompt-fragments.js";

export const SKILLS_DIRNAME = "skills";
export const SKILL_FILE = "SKILL.md";
/** Cap on the pinned skills-index block, mirrors memory-index cap. */
export const SKILLS_INDEX_MAX_CHARS = 4000;
/** Skill identifier shape — alnum + `_` + `-` + interior `.`, 1-64 chars. */
const VALID_SKILL_NAME = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$/;

export type SkillScope = "project" | "global" | "builtin";

/** inline = body enters parent log; subagent = isolated child loop, only final answer returns. */
export type SkillRunAs = "inline" | "subagent";

export interface Skill {
  /** Canonical name — sanitized, matches the directory / filename stem. */
  name: string;
  /** One-line description shown in the pinned index. */
  description: string;
  /** Full markdown body (post-frontmatter). Loaded on demand. */
  body: string;
  /** Which scope this skill was loaded from. */
  scope: SkillScope;
  /** Absolute path to the SKILL.md (or {name}.md) file, or "(builtin)" for shipped defaults. */
  path: string;
  /** Parsed `allowed-tools` frontmatter — when present, the spawned subagent's registry is scoped to these literal tool names. */
  allowedTools?: readonly string[];
  runAs: SkillRunAs;
  /** Subagent model override; only meaningful when `runAs === "subagent"`. */
  model?: string;
}

export interface SkillStoreOptions {
  /** Override `$HOME` — tests point this at a tmpdir. */
  homeDir?: string;
  /** Required for project-scope skills; omit to read only the global scope. */
  projectRoot?: string;
  /** Suppress bundled built-ins — for tests asserting exact list contents. */
  disableBuiltins?: boolean;
}

function parseFrontmatter(raw: string): { data: Record<string, string>; body: string } {
  const lines = raw.split(/\r?\n/);
  if (lines[0] !== "---") return { data: {}, body: raw };
  const end = lines.indexOf("---", 1);
  if (end < 0) return { data: {}, body: raw };
  const data: Record<string, string> = {};
  for (let i = 1; i < end; i++) {
    const line = lines[i];
    if (!line) continue;
    const m = line.match(/^([a-zA-Z_][a-zA-Z0-9_-]*):\s*(.*)$/);
    if (m?.[1]) data[m[1]] = (m[2] ?? "").trim();
  }
  return {
    data,
    body: lines
      .slice(end + 1)
      .join("\n")
      .replace(/^\n+/, ""),
  };
}

function isValidSkillName(name: string): boolean {
  return VALID_SKILL_NAME.test(name);
}

function parseAllowedTools(raw: string | undefined): readonly string[] | undefined {
  if (raw === undefined) return undefined;
  const names = raw
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
  return names.length > 0 ? Object.freeze(names) : undefined;
}

export class SkillStore {
  private readonly homeDir: string;
  private readonly projectRoot: string | undefined;
  private readonly disableBuiltins: boolean;

  constructor(opts: SkillStoreOptions = {}) {
    this.homeDir = opts.homeDir ?? homedir();
    this.projectRoot = opts.projectRoot ? resolve(opts.projectRoot) : undefined;
    this.disableBuiltins = opts.disableBuiltins === true;
  }

  /** True iff this store was configured with a project root. */
  hasProjectScope(): boolean {
    return this.projectRoot !== undefined;
  }

  /** Project scope first so per-repo skill overrides a global with the same name. */
  roots(): Array<{ dir: string; scope: SkillScope }> {
    const out: Array<{ dir: string; scope: SkillScope }> = [];
    if (this.projectRoot) {
      out.push({
        dir: join(this.projectRoot, ".reasonix", SKILLS_DIRNAME),
        scope: "project",
      });
    }
    out.push({ dir: join(this.homeDir, ".reasonix", SKILLS_DIRNAME), scope: "global" });
    return out;
  }

  /** Higher-priority root wins on collision (project > global > builtin); sorted for stable prefix hash. */
  list(): Skill[] {
    const byName = new Map<string, Skill>();
    for (const { dir, scope } of this.roots()) {
      if (!existsSync(dir)) continue;
      let entries: import("node:fs").Dirent[];
      try {
        entries = readdirSync(dir, { withFileTypes: true });
      } catch {
        continue;
      }
      for (const entry of entries) {
        const skill = this.readEntry(dir, scope, entry);
        if (!skill) continue;
        if (!byName.has(skill.name)) byName.set(skill.name, skill);
      }
    }
    // Builtins last so user/project files override on name collision.
    if (!this.disableBuiltins) {
      for (const skill of BUILTIN_SKILLS) {
        if (!byName.has(skill.name)) byName.set(skill.name, skill);
      }
    }
    return [...byName.values()].sort((a, b) => a.name.localeCompare(b.name));
  }

  /** Resolve one skill by name. Returns `null` if not found or malformed. */
  read(name: string): Skill | null {
    if (!isValidSkillName(name)) return null;
    for (const { dir, scope } of this.roots()) {
      if (!existsSync(dir)) continue;
      const dirCandidate = join(dir, name, SKILL_FILE);
      if (existsSync(dirCandidate) && statSync(dirCandidate).isFile()) {
        return this.parse(dirCandidate, name, scope);
      }
      const flatCandidate = join(dir, `${name}.md`);
      if (existsSync(flatCandidate) && statSync(flatCandidate).isFile()) {
        return this.parse(flatCandidate, name, scope);
      }
    }
    if (!this.disableBuiltins) {
      for (const skill of BUILTIN_SKILLS) {
        if (skill.name === name) return skill;
      }
    }
    return null;
  }

  private readEntry(dir: string, scope: SkillScope, entry: import("node:fs").Dirent): Skill | null {
    if (entry.isDirectory()) {
      if (!isValidSkillName(entry.name)) return null;
      const file = join(dir, entry.name, SKILL_FILE);
      if (!existsSync(file)) return null;
      return this.parse(file, entry.name, scope);
    }
    if (entry.isFile() && entry.name.endsWith(".md")) {
      const stem = entry.name.slice(0, -3);
      if (!isValidSkillName(stem)) return null;
      return this.parse(join(dir, entry.name), stem, scope);
    }
    return null;
  }

  private parse(path: string, stem: string, scope: SkillScope): Skill | null {
    let raw: string;
    try {
      raw = readFileSync(path, "utf8");
    } catch {
      return null;
    }
    const { data, body } = parseFrontmatter(raw);
    const name = data.name && isValidSkillName(data.name) ? data.name : stem;
    return {
      name,
      description: (data.description ?? "").trim(),
      body: body.trim(),
      scope,
      path,
      allowedTools: parseAllowedTools(data["allowed-tools"]),
      runAs: parseRunAs(data.runAs),
      model: data.model?.startsWith("deepseek-") ? data.model : undefined,
    };
  }
}

/** Unknown values default to the safe (non-spawning) `inline` mode. */
function parseRunAs(raw: string | undefined): SkillRunAs {
  return raw?.trim() === "subagent" ? "subagent" : "inline";
}

/** Subagent tag goes AFTER the name in brackets — leading-marker tags get copied into `name` arg verbatim. */
function skillIndexLine(s: Pick<Skill, "name" | "description" | "runAs">): string {
  const safeDesc = s.description.replace(/\n/g, " ").trim();
  const tag = s.runAs === "subagent" ? " [🧬 subagent]" : "";
  const max = 130 - s.name.length - tag.length;
  const clipped = safeDesc.length > max ? `${safeDesc.slice(0, Math.max(1, max - 1))}…` : safeDesc;
  return clipped ? `- ${s.name}${tag} — ${clipped}` : `- ${s.name}${tag}`;
}

/** Bodies stay out — prefix must stay short + cacheable; bodies load on demand. */
export function applySkillsIndex(basePrompt: string, opts: SkillStoreOptions = {}): string {
  const store = new SkillStore(opts);
  const skills = store.list().filter((s) => s.description);
  if (skills.length === 0) return basePrompt;
  const lines = skills.map(skillIndexLine);
  const joined = lines.join("\n");
  const truncated =
    joined.length > SKILLS_INDEX_MAX_CHARS
      ? `${joined.slice(0, SKILLS_INDEX_MAX_CHARS)}\n… (truncated ${
          joined.length - SKILLS_INDEX_MAX_CHARS
        } chars)`
      : joined;
  return [
    basePrompt,
    "",
    "# Skills — playbooks you can invoke",
    "",
    'One-liner index. Each entry is either a built-in or a user-authored playbook. Call `run_skill({ name: "<skill-name>", arguments: "<task>" })` — the `name` is JUST the skill identifier (e.g. `"explore"`), NOT the `[🧬 subagent]` tag that appears after it. Entries tagged `[🧬 subagent]` spawn an **isolated subagent** — its tool calls and reasoning never enter your context, only its final answer does. Use subagent skills for tasks that would otherwise flood your context (deep exploration, multi-step research, anything where you only need the conclusion). Plain skills are inlined: their body becomes a tool result you read and act on directly. The user can also invoke a skill via `/skill <name>`.',
    "",
    "```",
    truncated,
    "```",
  ].join("\n");
}

const BUILTIN_EXPLORE_BODY = `You are running as an exploration subagent. Your job is to investigate the codebase the parent agent pointed you at, then return one focused, distilled answer.

How to operate:
- Use read_file, search_files, search_content, directory_tree, list_directory, get_file_info as your primary tools. Stay read-only.
- For "find all places that call / reference / use X" questions, use \`search_content\` (content grep) — NOT \`search_files\` (which only matches file names). This is the most common subagent mistake; using the wrong tool gives empty results and you waste your iter budget chasing a phantom.
- Cast a wide net first (search_content for symbol references, directory_tree for structure) to map the territory; then read the 3-10 most relevant files in full.
- Don't read every file — be selective. Aim for breadth on the first pass, depth only where the question demands it.
- Stop exploring as soon as you can answer the question. The parent doesn't see your tool calls, so over-exploration is pure waste.

Your final answer:
- One paragraph (or a few short bullets). Lead with the conclusion.
- Cite specific file paths + line ranges when they support the answer.
- If the question can't be answered from what you found, say so plainly and suggest where to look next.
- No follow-up offers, no "let me know if you need more." The parent will ask again if they need more.

${NEGATIVE_CLAIM_RULE}

${TUI_FORMATTING_RULES}

The 'task' the parent gave you is the question you must answer. Treat any other reading of it as scope creep.`;

const BUILTIN_RESEARCH_BODY = `You are running as a research subagent. Your job is to gather information from code AND the web, synthesize it, and return one focused conclusion.

How to operate:
- Combine code reading (read_file, search_files) with web tools (web_search, web_fetch) as appropriate to the question.
- For "how does X work" / "is Y supported" questions: web first to find the canonical reference, then verify against the local code.
- For "what's our policy on Z" / "where do we use Q": local code first, web only if you need to compare against external standards.
- Cap yourself at ~10 tool calls. If you can't converge in 10, return what you have plus a note about what's missing.

Your final answer:
- One paragraph (or short bullets). Lead with the conclusion.
- Cite both code (file:line) AND web sources (URL) when they back the answer.
- Distinguish "I verified this in code" from "I read this on a docs page" — the parent will trust the former more.
- If the answer is uncertain, say so. Don't invent confidence.

${NEGATIVE_CLAIM_RULE}

${TUI_FORMATTING_RULES}

The 'task' the parent gave you is the research question. Stay on it.`;

const BUILTIN_REVIEW_BODY = `You are running as a code-review subagent. Your job is to inspect the changes the user is about to ship — usually the current git branch vs its upstream — and produce a focused review the parent can hand back to the user.

How to operate:
- Default scope: the current branch's diff vs the default branch. If the user's task names a specific commit range or files, honor that instead.
- Discover scope first: \`run_command git status\`, \`git diff --stat\`, \`git log --oneline\` to see what changed. Then \`git diff\` (or \`git diff <base>...HEAD\`) for the actual hunks.
- Read the touched files (\`read_file\`) when the diff alone doesn't carry enough context — function signatures, surrounding invariants, callers.
- For "any callers depending on this?" questions: \`search_content\` against the symbol BEFORE asserting impact.
- Stay read-only. Never \`run_command git commit\`, never write files, never propose SEARCH/REPLACE blocks. The parent decides whether to act on your findings.
- Cap yourself at ~12 tool calls. If the diff is too big to review in one pass, pick the riskiest 2-3 files and say so explicitly.

What to look for, in priority order:
1. **Correctness bugs** — off-by-one, null/undefined handling, race conditions, wrong sign / wrong operator, edge cases the code doesn't handle.
2. **Security** — injection (SQL, shell, path traversal), secrets in code, missing authz checks, unsafe deserialization.
3. **Behavior changes the diff hides** — renames that miss callers, removed branches that were load-bearing, error-handling that now swallows what used to surface.
4. **Tests** — does the change have tests for the new behavior? Are existing tests still meaningful, or did the change make them tautological?
5. **Style + consistency** — only flag deviations that matter (unsafe \`any\`, missing types in TypeScript, inconsistent error shape). Don't pile on cosmetic nits if the substance is clean.

Your final answer:
- Lead with a one-sentence verdict: "ship as-is" / "minor nits, OK to ship after" / "blocking issues, do not ship".
- Then a short bulleted list of issues, each with: file:line citation + the problem in one sentence + what to change.
- Group by severity if you have more than 4 items: **Blocking**, **Should-fix**, **Nits**.
- If everything looks clean, say so plainly. Don't manufacture concerns.

${NEGATIVE_CLAIM_RULE}

${TUI_FORMATTING_RULES}

The 'task' the parent gave you describes WHAT to review (a branch, a file set, or "the pending changes"). Stay on it; don't redesign the feature.`;

const BUILTIN_SECURITY_REVIEW_BODY = `You are running as a security-review subagent. Your job is to inspect the changes the user is about to ship — usually the current git branch vs its upstream — through a security lens specifically, and report exploitable issues.

How to operate:
- Default scope: the current branch's diff vs the default branch. If the user names a different range or a directory, honor that.
- Discover scope first: \`git status\`, \`git diff --stat\`, \`git diff <base>...HEAD\`. Read touched files (\`read_file\`) when the diff alone doesn't carry security context — auth checks, input validation, the actual handler that calls into the changed function.
- Use \`search_content\` to verify "is this user-controlled input ever sanitized later?" / "are there other call sites that depend on this validation?" before asserting impact.
- Stay read-only. Never write, never run destructive commands, never propose SEARCH/REPLACE blocks. The parent decides what to act on.
- Cap yourself at ~12 tool calls. If the diff is too big, focus on the riskiest 2-3 files and say so explicitly.

Threat model — flag with severity:

**CRITICAL** (do-not-ship):
- SQL / NoSQL / shell / template injection — user input concatenated into a query, command, or template without parameterization.
- Path traversal — user-controlled filenames touching the filesystem without canonicalization + sandbox check.
- Authentication / authorization missing — endpoints / actions that should require a session check but don't.
- Hardcoded secrets — API keys, passwords, signing tokens visible in the diff.
- Deserialization of untrusted input — \`pickle.loads\`, \`yaml.load\` (non-safe), \`eval\`, \`Function()\`, \`unserialize()\`.
- Cryptographic mistakes — homemade crypto, weak hashes (MD5/SHA-1) for passwords, missing IVs, ECB mode, predictable nonces.

**HIGH**:
- XSS — user input rendered into HTML without escaping (or wrong escaping context).
- SSRF — fetching URLs from user input without an allowlist.
- Race conditions in security-relevant code — TOCTOU on auth/file checks.
- Open redirects — user-controlled URL passed to a redirect helper.
- Insufficient logging on security events (login failure, permission denial) — only flag if the codebase clearly DOES log elsewhere.

**MEDIUM**:
- Verbose error messages leaking internal paths / stack traces / SQL.
- Missing rate limiting on a credential / token endpoint.
- Cross-origin / cookie-flag issues (missing \`Secure\` / \`HttpOnly\` / \`SameSite\`).

Things to NOT pile on (out of scope here — the regular /review covers them):
- Style, formatting, naming.
- Performance, refactor opportunities, test coverage gaps that aren't security-relevant.
- "Should be a constant" / "extract this helper" — irrelevant to ship-blocking.

Your final answer:
- Lead with a one-sentence verdict: "no security issues found", "minor concerns", or "blocking issues".
- Then a list grouped by severity. Each item: file:line + 1-sentence threat + 1-sentence fix direction (no full SEARCH/REPLACE — the user / parent agent will write that).
- If clean, say so plainly. Don't manufacture findings.

${NEGATIVE_CLAIM_RULE}

${TUI_FORMATTING_RULES}

The 'task' the parent gave you names what to review. Stay on it; don't redesign the feature.`;

const BUILTIN_TEST_BODY = `You are running as the parent agent — this skill is INLINED, not a subagent. The user invoked /test (or asked you to "run the tests and fix failures"). Your job: run the project's test suite, diagnose any failure, propose fixes as SEARCH/REPLACE edit blocks, then re-run. Repeat until green or you hit a wall you should escalate.

How to operate:

1. **Detect the test command**.
   - Look for \`package.json\` → \`scripts.test\` first (most common: \`npm test\`, \`pnpm test\`, \`yarn test\`).
   - If no package.json or no test script: try \`pytest\`, \`go test ./...\`, \`cargo test\` based on what files exist (pyproject.toml/requirements.txt → pytest; go.mod → go test; Cargo.toml → cargo test).
   - If you can't tell, ASK the user for the command — don't guess. One question, one tool call to confirm.

2. **Run it via run_command** (typical timeout 120s, bigger if the suite is large). Capture stdout + stderr.

3. **Read the failures**. Pull out: which test names failed, the actual error/traceback, the file + line that threw. Don't just paraphrase — locate the exact assertion or stack frame.

4. **Propose fixes**. For each distinct failure:
   - If the failure is in PRODUCTION code (test catches a real bug) → propose a SEARCH/REPLACE that fixes the production code.
   - If the failure is in TEST code (test is wrong, codebase is right) → propose a SEARCH/REPLACE that updates the test, AND say so explicitly: "This is a test bug, not a production bug — updating the assertion."
   - If the failure is environmental (missing dep, wrong node version, missing fixture file) → say so and stop. Don't try to install packages or change config without checking with the user.

5. **Apply + re-run**. After the user accepts the edit blocks, run the test command again. Iterate.

6. **Stop conditions**:
   - All tests pass → report green, summarize what changed.
   - Same test still failing after 2 fix attempts on the same line → STOP. Tell the user "I've tried twice, it's still failing — here's what I think is happening, want me to try a different angle?". Don't loop indefinitely.
   - 3+ unrelated failures → fix one at a time, smallest first, so each pass narrows the surface.

Don't:
- Run \`npm install\` / \`pip install\` / \`cargo update\` without asking — those mutate lockfiles and have global effects.
- Disable, skip, or delete failing tests to "make it green". If a test seems wrong, update its assertion with a one-sentence explanation, but never add \`.skip\` / \`it.skip\` / \`@pytest.mark.skip\`.
- Modify the test runner config (vitest.config, jest.config, etc.) to silence failures.

Lead each turn with a one-line status: "▸ running \`npm test\` ..." → "▸ 2 failures in tests/foo.test.ts — first is …" → so the user always knows where you are without scrolling tool output.`;

const BUILTIN_SKILLS: readonly Skill[] = Object.freeze([
  Object.freeze<Skill>({
    name: "explore",
    description:
      "Explore the codebase in an isolated subagent — wide-net read-only investigation that returns one distilled answer. Best for: 'find all places that...', 'how does X work across the project', 'survey the code for Y'.",
    body: BUILTIN_EXPLORE_BODY,
    scope: "builtin",
    path: "(builtin)",
    runAs: "subagent",
  }),
  Object.freeze<Skill>({
    name: "research",
    description:
      "Research a question by combining web search + code reading in an isolated subagent. Best for: 'is X feature supported by lib Y', 'what's the canonical way to do Z', 'compare our impl against the spec'.",
    body: BUILTIN_RESEARCH_BODY,
    scope: "builtin",
    path: "(builtin)",
    runAs: "subagent",
  }),
  Object.freeze<Skill>({
    name: "review",
    description:
      "Review the pending changes (current branch diff by default) in an isolated subagent — flags correctness, security, missing tests, hidden behavior changes; reports verdict + per-issue file:line. Read-only; the parent decides what to act on.",
    body: BUILTIN_REVIEW_BODY,
    scope: "builtin",
    path: "(builtin)",
    runAs: "subagent",
  }),
  Object.freeze<Skill>({
    name: "security-review",
    description:
      "Security-focused review of the current branch diff in an isolated subagent — flags injection/authz/secrets/deserialization/path-traversal/crypto issues, severity-tagged. Read-only. Use when shipping changes that touch auth, input parsing, file IO, or external requests.",
    body: BUILTIN_SECURITY_REVIEW_BODY,
    scope: "builtin",
    path: "(builtin)",
    runAs: "subagent",
  }),
  Object.freeze<Skill>({
    name: "test",
    description:
      "Run the project's test suite, diagnose failures, propose SEARCH/REPLACE fixes, re-run until green (or stop after 2 fix attempts on the same failure). Inlined — runs in the parent loop so you see the edit blocks and can /apply them. Detects npm/pnpm/yarn/pytest/go/cargo.",
    body: BUILTIN_TEST_BODY,
    scope: "builtin",
    path: "(builtin)",
    runAs: "inline",
  }),
]);
