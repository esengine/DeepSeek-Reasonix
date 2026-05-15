/** User-private memory pinned into the immutable prefix; distinct from committable REASONIX.md. */

import { createHash } from "node:crypto";
import {
  existsSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { homedir } from "node:os";
import { join, resolve } from "node:path";
import { type ReasonixConfig, memoryTypeDefaults } from "../config.js";
import { parseFrontmatter } from "../frontmatter.js";
import { applySkillsIndex } from "../skills.js";
import { applyProjectMemory, memoryEnabled } from "./project.js";

export const USER_MEMORY_DIR = "memory";
export const MEMORY_INDEX_FILE = "MEMORY.md";
/** Cap on the index file content loaded into the prefix, per scope. */
export const MEMORY_INDEX_MAX_CHARS = 4000;

export const BUILTIN_MEMORY_TYPES = ["user", "feedback", "project", "reference"] as const;
export type BuiltinMemoryType = (typeof BUILTIN_MEMORY_TYPES)[number];
/** Built-ins plus any string declared in `config.memory.customTypes`. Unknown values are accepted (round-tripped verbatim). */
export type MemoryType = BuiltinMemoryType | (string & {});
export type MemoryScope = "global" | "project";
export type MemoryPriority = "low" | "medium" | "high";
export type MemoryExpires = "project_end";

export interface MemoryEntry {
  name: string;
  type: MemoryType;
  scope: MemoryScope;
  description: string;
  body: string;
  /** ISO date string (YYYY-MM-DD). */
  createdAt: string;
  /** Explicit per-entry priority; absent → resolve from config default for `type`, else "medium". */
  priority?: MemoryPriority;
  /** Lifecycle hint. `project_end` → cleared by `/memory clear project`. */
  expires?: MemoryExpires;
}

export interface MemoryStoreOptions {
  /** Override `~/.reasonix` — tests set this to a tmpdir. */
  homeDir?: string;
  /** Absolute sandbox root. Required to use `scope: "project"`. */
  projectRoot?: string;
}

export interface WriteInput {
  name: string;
  type: MemoryType;
  scope: MemoryScope;
  description: string;
  body: string;
  priority?: MemoryPriority;
  expires?: MemoryExpires;
}

const VALID_NAME = /^[a-zA-Z0-9_-][a-zA-Z0-9_.-]{1,38}[a-zA-Z0-9]$/;

/** Throws on path-injection (../, /, leading dot). Allowed: 3-40 chars, alnum/_/-, interior `.`. */
export function sanitizeMemoryName(raw: string): string {
  const trimmed = String(raw ?? "").trim();
  if (!VALID_NAME.test(trimmed)) {
    throw new Error(
      `invalid memory name: ${JSON.stringify(raw)} — must be 3-40 chars, alnum/_/-, no path separators`,
    );
  }
  return trimmed;
}

/** Stable 16-hex-char hash of an absolute sandbox root path. */
export function projectHash(rootDir: string): string {
  const abs = resolve(rootDir);
  return createHash("sha1").update(abs).digest("hex").slice(0, 16);
}

function scopeDir(opts: { homeDir: string; scope: MemoryScope; projectRoot?: string }): string {
  if (opts.scope === "global") {
    return join(opts.homeDir, USER_MEMORY_DIR, "global");
  }
  if (!opts.projectRoot) {
    throw new Error("scope=project requires a projectRoot on MemoryStore");
  }
  return join(opts.homeDir, USER_MEMORY_DIR, projectHash(opts.projectRoot));
}

function ensureDir(p: string): void {
  if (!existsSync(p)) mkdirSync(p, { recursive: true });
}

function formatFrontmatter(e: WriteInput & { createdAt: string }): string {
  const lines = [
    "---",
    `name: ${e.name}`,
    `description: ${e.description.replace(/\n/g, " ")}`,
    `type: ${e.type}`,
    `scope: ${e.scope}`,
    `created: ${e.createdAt}`,
  ];
  if (e.priority) lines.push(`priority: ${e.priority}`);
  if (e.expires) lines.push(`expires: ${e.expires}`);
  lines.push("---", "");
  return lines.join("\n");
}

function coercePriority(v: unknown): MemoryPriority | undefined {
  return v === "low" || v === "medium" || v === "high" ? v : undefined;
}

function coerceExpires(v: unknown): MemoryExpires | undefined {
  return v === "project_end" ? v : undefined;
}

function todayIso(): string {
  const d = new Date();
  return d.toISOString().slice(0, 10);
}

function indexLine(e: Pick<MemoryEntry, "name" | "description">): string {
  const safeDesc = e.description.replace(/\n/g, " ").trim();
  const max = 130 - e.name.length;
  const clipped = safeDesc.length > max ? `${safeDesc.slice(0, Math.max(1, max - 1))}…` : safeDesc;
  return `- [${e.name}](${e.name}.md) — ${clipped}`;
}

export class MemoryStore {
  private readonly homeDir: string;
  private readonly projectRoot: string | undefined;

  constructor(opts: MemoryStoreOptions = {}) {
    this.homeDir = opts.homeDir ?? join(homedir(), ".reasonix");
    this.projectRoot = opts.projectRoot ? resolve(opts.projectRoot) : undefined;
  }

  /** Directory this store writes `scope` files into, creating it if needed. */
  dir(scope: MemoryScope): string {
    const d = scopeDir({ homeDir: this.homeDir, scope, projectRoot: this.projectRoot });
    ensureDir(d);
    return d;
  }

  /** Absolute path to a memory file (no existence check). */
  pathFor(scope: MemoryScope, name: string): string {
    return join(this.dir(scope), `${sanitizeMemoryName(name)}.md`);
  }

  /** True iff this store is configured with a project scope available. */
  hasProjectScope(): boolean {
    return this.projectRoot !== undefined;
  }

  loadIndex(
    scope: MemoryScope,
  ): { content: string; originalChars: number; truncated: boolean } | null {
    if (scope === "project" && !this.projectRoot) return null;
    const file = join(
      scopeDir({ homeDir: this.homeDir, scope, projectRoot: this.projectRoot }),
      MEMORY_INDEX_FILE,
    );
    if (!existsSync(file)) return null;
    let raw: string;
    try {
      raw = readFileSync(file, "utf8");
    } catch {
      return null;
    }
    const trimmed = raw.trim();
    if (!trimmed) return null;
    const originalChars = trimmed.length;
    const truncated = originalChars > MEMORY_INDEX_MAX_CHARS;
    const content = truncated
      ? `${trimmed.slice(0, MEMORY_INDEX_MAX_CHARS)}\n… (truncated ${originalChars - MEMORY_INDEX_MAX_CHARS} chars)`
      : trimmed;
    return { content, originalChars, truncated };
  }

  /** Read one memory file's body (frontmatter stripped). Throws if missing. */
  read(scope: MemoryScope, name: string): MemoryEntry {
    const file = this.pathFor(scope, name);
    if (!existsSync(file)) {
      throw new Error(`memory not found: scope=${scope} name=${name}`);
    }
    const raw = readFileSync(file, "utf8");
    const { data, body } = parseFrontmatter(raw);
    const entry: MemoryEntry = {
      name: data.name ?? name,
      type: (data.type as MemoryType) ?? "project",
      scope: (data.scope as MemoryScope) ?? scope,
      description: data.description ?? "",
      body: body.trim(),
      createdAt: data.created ?? "",
    };
    const priority = coercePriority(data.priority);
    if (priority) entry.priority = priority;
    const expires = coerceExpires(data.expires);
    if (expires) entry.expires = expires;
    return entry;
  }

  /** Skips malformed files — index stays queryable even if one file is hand-edited into nonsense. */
  list(): MemoryEntry[] {
    const out: MemoryEntry[] = [];
    const scopes: MemoryScope[] = this.projectRoot ? ["global", "project"] : ["global"];
    for (const scope of scopes) {
      const dir = scopeDir({ homeDir: this.homeDir, scope, projectRoot: this.projectRoot });
      if (!existsSync(dir)) continue;
      let entries: string[];
      try {
        entries = readdirSync(dir);
      } catch {
        continue;
      }
      for (const entry of entries) {
        if (entry === MEMORY_INDEX_FILE) continue;
        if (!entry.endsWith(".md")) continue;
        const name = entry.slice(0, -3);
        try {
          out.push(this.read(scope, name));
        } catch {
          // malformed file — skip rather than fail the whole list
        }
      }
    }
    return out;
  }

  write(input: WriteInput): string {
    if (input.scope === "project" && !this.projectRoot) {
      throw new Error("cannot write project-scoped memory: no projectRoot configured");
    }
    const name = sanitizeMemoryName(input.name);
    const desc = String(input.description ?? "").trim();
    if (!desc) throw new Error("memory description cannot be empty");
    const body = String(input.body ?? "").trim();
    if (!body) throw new Error("memory body cannot be empty");
    const entry: WriteInput & { createdAt: string } = {
      ...input,
      name,
      description: desc,
      body,
      createdAt: todayIso(),
    };
    if (input.priority) entry.priority = input.priority;
    if (input.expires) entry.expires = input.expires;
    const dir = this.dir(input.scope);
    const file = join(dir, `${name}.md`);
    const content = `${formatFrontmatter(entry)}${body}\n`;
    writeFileSync(file, content, "utf8");
    this.regenerateIndex(input.scope);
    return file;
  }

  /** Delete one memory + its index line. No-op if the file is already gone. */
  delete(scope: MemoryScope, rawName: string): boolean {
    if (scope === "project" && !this.projectRoot) {
      throw new Error("cannot delete project-scoped memory: no projectRoot configured");
    }
    const file = this.pathFor(scope, rawName);
    if (!existsSync(file)) return false;
    unlinkSync(file);
    this.regenerateIndex(scope);
    return true;
  }

  /** Sorted by name — same file set must produce byte-identical MEMORY.md for stable prefix hashing. */
  private regenerateIndex(scope: MemoryScope): void {
    const dir = scopeDir({ homeDir: this.homeDir, scope, projectRoot: this.projectRoot });
    if (!existsSync(dir)) return;
    let files: string[];
    try {
      files = readdirSync(dir);
    } catch {
      return;
    }
    const mdFiles = files
      .filter((f) => f !== MEMORY_INDEX_FILE && f.endsWith(".md"))
      .sort((a, b) => a.localeCompare(b));
    const indexPath = join(dir, MEMORY_INDEX_FILE);
    if (mdFiles.length === 0) {
      if (existsSync(indexPath)) unlinkSync(indexPath);
      return;
    }
    const lines: string[] = [];
    for (const f of mdFiles) {
      const name = f.slice(0, -3);
      try {
        const entry = this.read(scope, name);
        lines.push(indexLine({ name: entry.name || name, description: entry.description }));
      } catch {
        // Malformed: still surface it in the index so the user notices.
        lines.push(`- [${name}](${name}.md) — (malformed, check frontmatter)`);
      }
    }
    writeFileSync(indexPath, `${lines.join("\n")}\n`, "utf8");
  }
}

/** Freeform `#g` destination, distinct from MEMORY.md's curated index of named files. */
export function readGlobalReasonixMemory(
  homeDir: string = join(homedir(), ".reasonix"),
): { path: string; content: string; originalChars: number; truncated: boolean } | null {
  const path = join(homeDir, "REASONIX.md");
  if (!existsSync(path)) return null;
  let raw: string;
  try {
    raw = readFileSync(path, "utf8");
  } catch {
    return null;
  }
  const trimmed = raw.trim();
  if (!trimmed) return null;
  const originalChars = trimmed.length;
  // Reuse the project-memory cap so both freeform files have the same
  // headroom (8000 chars ≈ 2k tokens). They serve the same purpose at
  // different scopes.
  const truncated = originalChars > 8000;
  const content = truncated
    ? `${trimmed.slice(0, 8000)}\n… (truncated ${originalChars - 8000} chars)`
    : trimmed;
  return { path, content, originalChars, truncated };
}

export function applyGlobalReasonixMemory(basePrompt: string, homeDir?: string): string {
  if (!memoryEnabled()) return basePrompt;
  const dir = homeDir ?? join(homedir(), ".reasonix");
  const mem = readGlobalReasonixMemory(dir);
  if (!mem) return basePrompt;
  return [
    basePrompt,
    "",
    "# Global memory (~/.reasonix/REASONIX.md)",
    "",
    "Cross-project notes the user pinned via the `#g` prompt prefix. Treat as authoritative — same level of trust as project memory.",
    "",
    "```",
    mem.content,
    "```",
  ].join("\n");
}

/** Effective priority: entry's own field wins, else the config default for its type, else undefined. */
export function effectivePriority(
  entry: MemoryEntry,
  cfg?: ReasonixConfig,
): MemoryPriority | undefined {
  if (entry.priority) return entry.priority;
  return memoryTypeDefaults(entry.type, cfg).priority;
}

function highPriorityBlock(entries: MemoryEntry[], cfg?: ReasonixConfig): string | null {
  const high = entries.filter((e) => effectivePriority(e, cfg) === "high");
  if (high.length === 0) return null;
  const lines: string[] = [
    "# HIGH PRIORITY constraints (must observe)",
    "",
    "These memories were declared `priority: high` (via config.memory.customTypes or the memory file itself). Treat them as hard rules — violations override any other guidance below.",
    "",
  ];
  for (const e of high) {
    const head = `!!! [${e.scope}/${e.type}/${e.name}] ${e.description || "(no description)"}`;
    lines.push(head);
    if (e.body) lines.push("", e.body);
    lines.push("");
  }
  return lines.join("\n").trimEnd();
}

/** Empty index → omit the whole block (otherwise we'd add bytes to the prefix hash for nothing). */
export function applyUserMemory(
  basePrompt: string,
  opts: { homeDir?: string; projectRoot?: string; cfg?: ReasonixConfig } = {},
): string {
  if (!memoryEnabled()) return basePrompt;
  const store = new MemoryStore(opts);
  const global = store.loadIndex("global");
  const project = store.hasProjectScope() ? store.loadIndex("project") : null;
  const high = highPriorityBlock(store.list(), opts.cfg);
  if (!global && !project && !high) return basePrompt;
  const parts: string[] = [basePrompt];
  if (high) parts.push("", high);
  if (global) {
    parts.push(
      "",
      "# User memory — global (~/.reasonix/memory/global/MEMORY.md)",
      "",
      "Cross-project facts and preferences the user has told you in prior sessions. TREAT AS AUTHORITATIVE — don't re-verify via filesystem or web. One-liners index detail files; call `recall_memory` for full bodies only when the one-liner isn't enough.",
      "",
      "```",
      global.content,
      "```",
    );
  }
  if (project) {
    parts.push(
      "",
      "# User memory — this project",
      "",
      "Per-project facts the user established in prior sessions (not committed to the repo). TREAT AS AUTHORITATIVE. Same recall pattern as global memory.",
      "",
      "```",
      project.content,
      "```",
    );
  }
  return parts.join("\n");
}

export function applyMemoryStack(
  basePrompt: string,
  rootDir: string,
  opts: { homeDir?: string } = {},
): string {
  const withProject = applyProjectMemory(basePrompt, rootDir);
  const withGlobal = applyGlobalReasonixMemory(
    withProject,
    opts.homeDir ? join(opts.homeDir, ".reasonix") : undefined,
  );
  const withMemory = applyUserMemory(withGlobal, {
    projectRoot: rootDir,
    homeDir: opts.homeDir,
  });
  return applySkillsIndex(withMemory, { projectRoot: rootDir, homeDir: opts.homeDir });
}
