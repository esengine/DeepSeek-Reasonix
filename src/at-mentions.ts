/** Expand `@path` mentions inline. Paths must resolve inside rootDir; escapes / oversize get a skip note, not content. */

import { type Dirent, existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { readdir, stat } from "node:fs/promises";
import { isAbsolute, join, relative, resolve } from "node:path";
import {
  type GitignoreLayer,
  ignoredByLayers,
  loadGitignoreAt,
  loadGitignoreAtSync,
} from "./gitignore.js";

/** Caps match tool-result dispatch truncation (0.5.2). */
export const DEFAULT_AT_MENTION_MAX_BYTES = 64 * 1024;

/** Cap on entries returned for a `@<dir>` listing. ~200 paths × ~50 chars ≈ 10 KB — fits inside DEFAULT_AT_MENTION_MAX_BYTES with room for the rest of the prompt. */
export const DEFAULT_AT_DIR_MAX_ENTRIES = 200;

/** Universally-uninteresting build / VCS dirs. Framework-specific dirs (Pods, target, …) live in .gitignore. */
export const DEFAULT_PICKER_IGNORE_DIRS: readonly string[] = [
  "node_modules",
  ".git",
  "dist",
  "build",
  ".next",
  "out",
  "coverage",
  ".cache",
  ".vscode",
  ".idea",
  "target",
  ".venv",
  "venv",
  "__pycache__",
];

export interface ListFilesOptions {
  /** Cap the walk once we've collected this many entries. Default 2000. */
  maxResults?: number;
  /** Directory names to skip entirely. Defaults to {@link DEFAULT_PICKER_IGNORE_DIRS}. */
  ignoreDirs?: readonly string[];
  /** Walk nested .gitignores (root + every subdir). Default true. */
  respectGitignore?: boolean;
}

/** Sync on purpose — fits the TUI's single-turn-per-tick model. Skips dot-DIRS but keeps dotfiles. */
export function listFilesSync(root: string, opts: ListFilesOptions = {}): string[] {
  return listFilesWithStatsSync(root, opts).map((e) => e.path);
}

export interface FileWithStats {
  /** Relative path with forward-slash separator. */
  path: string;
  /** Modification time (Date.getTime() / ms since epoch). 0 when stat failed. */
  mtimeMs: number;
}

/** Stat failures kept as `mtimeMs: 0` — entry still appears, sinks to bottom of recency sort. */
export function listFilesWithStatsSync(root: string, opts: ListFilesOptions = {}): FileWithStats[] {
  const maxResults = Math.max(1, opts.maxResults ?? 2000);
  const ignoreDirs = new Set(opts.ignoreDirs ?? DEFAULT_PICKER_IGNORE_DIRS);
  const rootAbs = resolve(root);
  const respectGi = opts.respectGitignore !== false;
  const out: FileWithStats[] = [];

  const walk = (dirAbs: string, dirRel: string, layers: readonly GitignoreLayer[]) => {
    if (out.length >= maxResults) return;
    let effectiveLayers = layers;
    if (respectGi) {
      const ig = loadGitignoreAtSync(dirAbs);
      if (ig) effectiveLayers = [...layers, { dirAbs, ig }];
    }
    let entries: Dirent[];
    try {
      entries = readdirSync(dirAbs, { withFileTypes: true });
    } catch {
      return;
    }
    entries.sort((a, b) => a.name.localeCompare(b.name));
    for (const ent of entries) {
      if (out.length >= maxResults) return;
      const relPath = dirRel ? `${dirRel}/${ent.name}` : ent.name;
      const absPath = join(dirAbs, ent.name);
      if (ent.isDirectory()) {
        if (ent.name.startsWith(".") || ignoreDirs.has(ent.name)) continue;
        if (ignoredByLayers(effectiveLayers, absPath, true)) continue;
        walk(absPath, relPath, effectiveLayers);
      } else if (ent.isFile()) {
        if (ignoredByLayers(effectiveLayers, absPath, false)) continue;
        let mtimeMs = 0;
        try {
          mtimeMs = statSync(absPath).mtimeMs;
        } catch {
          /* stat failed (permission / EAGAIN) — keep the entry with mtime=0 */
        }
        out.push({ path: relPath, mtimeMs });
      } else if (ent.isSymbolicLink()) {
        // Dirent.isFile() returns false for symlinks even when they point at
        // regular files — stat the target to recover them. Symlinks-to-dirs
        // are not followed (cycle risk).
        let target: ReturnType<typeof statSync> | null = null;
        try {
          target = statSync(absPath);
        } catch {
          continue;
        }
        if (!target.isFile()) continue;
        if (ignoredByLayers(effectiveLayers, absPath, false)) continue;
        out.push({ path: relPath, mtimeMs: target.mtimeMs });
      }
    }
  };

  walk(rootAbs, "", []);
  return out;
}

/** Parallel stat per directory — Windows stat syscalls are 3-5× slower than Linux. */
export async function listFilesWithStatsAsync(
  root: string,
  opts: ListFilesOptions = {},
): Promise<FileWithStats[]> {
  const out: FileWithStats[] = [];
  const maxResults = Math.max(1, opts.maxResults ?? 2000);
  await walkFilesStream(root, {
    ...opts,
    onEntry: (e) => {
      out.push(e);
      return out.length < maxResults;
    },
  });
  return out;
}

export interface StreamWalkOptions {
  ignoreDirs?: readonly string[];
  respectGitignore?: boolean;
  signal?: AbortSignal;
  /** Called per file entry. Return false to halt the walk. */
  onEntry: (entry: FileWithStats) => boolean | undefined;
  /** Called periodically with the running file-count. */
  onProgress?: (scanned: number) => void;
  /** Default 100ms — minimum gap between onProgress calls. */
  progressIntervalMs?: number;
}

/** Cancelable, streaming walker. Drives `listFilesWithStatsAsync` and the picker's search-mode walk. */
export async function walkFilesStream(
  root: string,
  opts: StreamWalkOptions,
): Promise<{ scanned: number; cancelled: boolean }> {
  const ignoreDirs = new Set(opts.ignoreDirs ?? DEFAULT_PICKER_IGNORE_DIRS);
  const respectGi = opts.respectGitignore !== false;
  const rootAbs = resolve(root);
  const progressGap = Math.max(0, opts.progressIntervalMs ?? 100);
  let scanned = 0;
  let halted = false;
  let lastProgress = 0;

  const reportProgress = (force: boolean) => {
    if (!opts.onProgress) return;
    const now = Date.now();
    if (force || now - lastProgress >= progressGap) {
      lastProgress = now;
      opts.onProgress(scanned);
    }
  };

  const emit = (entry: FileWithStats) => {
    scanned++;
    if (halted) return;
    if (opts.onEntry(entry) === false) halted = true;
    reportProgress(false);
  };

  const walk = async (
    dirAbs: string,
    dirRel: string,
    layers: readonly GitignoreLayer[],
  ): Promise<void> => {
    if (halted || opts.signal?.aborted) return;
    let effectiveLayers = layers;
    if (respectGi) {
      const ig = await loadGitignoreAt(dirAbs);
      if (ig) effectiveLayers = [...layers, { dirAbs, ig }];
    }
    let entries: Dirent[];
    try {
      entries = await readdir(dirAbs, { withFileTypes: true });
    } catch {
      return;
    }
    entries.sort((a, b) => a.name.localeCompare(b.name));
    const fileEnts: Dirent[] = [];
    for (const ent of entries) {
      if (halted || opts.signal?.aborted) break;
      const absPath = join(dirAbs, ent.name);
      if (ent.isDirectory()) {
        if (ent.name.startsWith(".") || ignoreDirs.has(ent.name)) continue;
        if (ignoredByLayers(effectiveLayers, absPath, true)) continue;
        if (fileEnts.length > 0) {
          await flushFiles(fileEnts, dirAbs, dirRel, effectiveLayers, emit);
          fileEnts.length = 0;
          if (halted || opts.signal?.aborted) return;
        }
        await walk(absPath, dirRel ? `${dirRel}/${ent.name}` : ent.name, effectiveLayers);
      } else if (ent.isFile() || ent.isSymbolicLink()) {
        fileEnts.push(ent);
      }
    }
    if (fileEnts.length > 0 && !halted && !opts.signal?.aborted) {
      await flushFiles(fileEnts, dirAbs, dirRel, effectiveLayers, emit);
    }
  };

  await walk(rootAbs, "", []);
  reportProgress(true);
  return { scanned, cancelled: !!opts.signal?.aborted };
}

async function flushFiles(
  ents: readonly Dirent[],
  dirAbs: string,
  dirRel: string,
  layers: readonly GitignoreLayer[],
  emit: (e: FileWithStats) => void,
): Promise<void> {
  const accepted = ents.filter((e) => !ignoredByLayers(layers, join(dirAbs, e.name), false));
  const stats = await Promise.all(
    accepted.map((e) =>
      stat(join(dirAbs, e.name))
        .then((s) => ({ mtimeMs: s.mtimeMs, isFile: s.isFile() }))
        .catch(() => null),
    ),
  );
  for (let i = 0; i < accepted.length; i++) {
    const ent = accepted[i]!;
    const s = stats[i];
    if (ent.isSymbolicLink() && (!s || !s.isFile)) continue;
    emit({
      path: dirRel ? `${dirRel}/${ent.name}` : ent.name,
      mtimeMs: s?.mtimeMs ?? 0,
    });
  }
}

export interface DirEntry {
  name: string;
  /** Relative-to-root path (forward slashes). For dirs, no trailing slash. */
  path: string;
  isDir: boolean;
  /** 0 for directories (no stat), real mtime for files. */
  mtimeMs: number;
}

export interface ListDirectoryOptions {
  ignoreDirs?: readonly string[];
  respectGitignore?: boolean;
}

/** One-level browse for the @-picker. Folders first then files, alpha within each group. Resolves outside-root to []. */
export async function listDirectory(
  root: string,
  relDir: string,
  opts: ListDirectoryOptions = {},
): Promise<DirEntry[]> {
  const ignoreDirs = new Set(opts.ignoreDirs ?? DEFAULT_PICKER_IGNORE_DIRS);
  const respectGi = opts.respectGitignore !== false;
  const rootAbs = resolve(root);
  const dirAbs = resolve(rootAbs, relDir);
  const rel = relative(rootAbs, dirAbs);
  if (rel.startsWith("..") || isAbsolute(rel)) return [];

  const layers: GitignoreLayer[] = [];
  if (respectGi) {
    const segs = rel ? rel.split(/[\\/]/) : [];
    let cursor = rootAbs;
    const ig = await loadGitignoreAt(cursor);
    if (ig) layers.push({ dirAbs: cursor, ig });
    for (const seg of segs) {
      cursor = join(cursor, seg);
      const igSeg = await loadGitignoreAt(cursor);
      if (igSeg) layers.push({ dirAbs: cursor, ig: igSeg });
    }
  }

  let raw: Dirent[];
  try {
    raw = await readdir(dirAbs, { withFileTypes: true });
  } catch {
    return [];
  }
  const dirRel = rel.split(/[\\/]/).join("/");
  const dirs: DirEntry[] = [];
  const files: Dirent[] = [];
  for (const ent of raw) {
    const absPath = join(dirAbs, ent.name);
    if (ent.isDirectory()) {
      if (ent.name.startsWith(".") || ignoreDirs.has(ent.name)) continue;
      if (ignoredByLayers(layers, absPath, true)) continue;
      dirs.push({
        name: ent.name,
        path: dirRel ? `${dirRel}/${ent.name}` : ent.name,
        isDir: true,
        mtimeMs: 0,
      });
    } else if (ent.isFile() || ent.isSymbolicLink()) {
      if (ignoredByLayers(layers, absPath, false)) continue;
      files.push(ent);
    }
  }
  const stats = await Promise.all(
    files.map((e) =>
      stat(join(dirAbs, e.name))
        .then((s) => ({ mtimeMs: s.mtimeMs, isFile: s.isFile() }))
        .catch(() => null),
    ),
  );
  const fileEntries: DirEntry[] = [];
  for (let i = 0; i < files.length; i++) {
    const ent = files[i]!;
    const s = stats[i];
    if (ent.isSymbolicLink() && (!s || !s.isFile)) continue;
    fileEntries.push({
      name: ent.name,
      path: dirRel ? `${dirRel}/${ent.name}` : ent.name,
      isDir: false,
      mtimeMs: s?.mtimeMs ?? 0,
    });
  }
  dirs.sort((a, b) => a.name.localeCompare(b.name));
  fileEntries.sort((a, b) => a.name.localeCompare(b.name));
  return [...dirs, ...fileEntries];
}

export interface ParsedAtQuery {
  /** Directory portion (rel from root, no trailing slash). Empty = root. */
  dir: string;
  /** Filter portion — chars after the last slash. Empty if query ended in `/`. */
  filter: string;
  /** True if the query ended in `/` — caller knows to browse `dir`. */
  trailingSlash: boolean;
}

/** Split `src/auth/log` → `{dir: "src/auth", filter: "log"}`; trailing slash sets `trailingSlash` and clears filter. */
export function parseAtQuery(query: string): ParsedAtQuery {
  const normalized = query.replace(/\\/g, "/");
  const trailingSlash = normalized.endsWith("/");
  const trimmed = trailingSlash ? normalized.slice(0, -1) : normalized;
  const lastSlash = trimmed.lastIndexOf("/");
  if (trailingSlash) return { dir: trimmed, filter: "", trailingSlash: true };
  if (lastSlash < 0) return { dir: "", filter: trimmed, trailingSlash: false };
  return {
    dir: trimmed.slice(0, lastSlash),
    filter: trimmed.slice(lastSlash + 1),
    trailingSlash: false,
  };
}

/** Trailing-token only, anchored at end-of-input — distinct from `AT_MENTION_PATTERN` which scans all. `\p{L}\p{N}` for CJK and other non-ASCII filenames. */
export const AT_PICKER_PREFIX = /(?:^|\s)@([\p{L}\p{N}_./\\-]*)$/u;

export function detectAtPicker(input: string): { query: string; atOffset: number } | null {
  const m = AT_PICKER_PREFIX.exec(input);
  if (!m) return null;
  const query = m[1] ?? "";
  // `m.index` is the offset of the capture group's SURROUNDING match —
  // which starts at either ^ or the preceding whitespace. The `@`
  // itself is at `end-of-input - query.length - 1`.
  const atOffset = input.length - query.length - 1;
  return { query, atOffset };
}

/** A candidate accepted by the picker ranker — either a bare path or a path with mtime. */
export type PickerCandidate = string | FileWithStats;

export interface RankPickerOptions {
  /** Upper bound on returned entries. Default 40. */
  limit?: number;
  recentlyUsed?: readonly string[];
}

export function rankPickerCandidates(
  files: readonly PickerCandidate[],
  query: string,
  limitOrOpts?: number | RankPickerOptions,
): string[] {
  const opts: RankPickerOptions =
    typeof limitOrOpts === "number" ? { limit: limitOrOpts } : (limitOrOpts ?? {});
  const limit = opts.limit ?? 40;
  const recent = new Set(opts.recentlyUsed ?? []);

  const entries: FileWithStats[] = files.map((f) =>
    typeof f === "string" ? { path: f, mtimeMs: 0 } : f,
  );

  if (!query) {
    // Only re-sort when we actually have signal to sort by. If input
    // is bare strings (mtime = 0 everywhere) AND there's no recent-
    // used list, preserve input order so callers keep their existing
    // layout. Passing FileWithStats or a non-empty recentlyUsed opts
    // you into mtime+recency ranking.
    const anyMtime = entries.some((e) => e.mtimeMs > 0);
    if (!anyMtime && recent.size === 0) {
      return entries.slice(0, limit).map((e) => e.path);
    }
    const sorted = [...entries].sort((a, b) => {
      const aRecent = recent.has(a.path) ? 1 : 0;
      const bRecent = recent.has(b.path) ? 1 : 0;
      if (aRecent !== bRecent) return bRecent - aRecent;
      if (a.mtimeMs !== b.mtimeMs) return b.mtimeMs - a.mtimeMs;
      return a.path.localeCompare(b.path);
    });
    return sorted.slice(0, limit).map((e) => e.path);
  }

  const needle = query.toLowerCase();
  const scored: Array<{ path: string; score: number; mtimeMs: number; recent: boolean }> = [];
  for (const e of entries) {
    const lower = e.path.toLowerCase();
    const hit = lower.indexOf(needle);
    if (hit >= 0) {
      const slash = lower.lastIndexOf("/");
      const base = slash >= 0 ? lower.slice(slash + 1) : lower;
      let cls = 2;
      if (base.startsWith(needle)) cls = 0;
      else if (lower.startsWith(needle)) cls = 1;
      scored.push({
        path: e.path,
        score: cls * 10_000 + Math.min(hit, 9999),
        mtimeMs: e.mtimeMs,
        recent: recent.has(e.path),
      });
      continue;
    }
    const fuzzy = fuzzySubseqScore(needle, lower);
    if (fuzzy === null) continue;
    scored.push({
      path: e.path,
      score: 30_000 + fuzzy,
      mtimeMs: e.mtimeMs,
      recent: recent.has(e.path),
    });
  }
  scored.sort((a, b) => {
    if (a.score !== b.score) return a.score - b.score;
    // Tie-break: recently-used, then mtime (newer first).
    if (a.recent !== b.recent) return a.recent ? -1 : 1;
    return b.mtimeMs - a.mtimeMs;
  });
  return scored.slice(0, limit).map((s) => s.path);
}

function fuzzySubseqScore(needle: string, target: string): number | null {
  if (needle.length === 0) return 0;
  const slashIdx = target.lastIndexOf("/");
  const basenameStart = slashIdx >= 0 ? slashIdx + 1 : 0;
  let qi = 0;
  let lastMatchIdx = -2;
  let consecutive = 0;
  let basenameMatches = 0;
  let totalGap = 0;
  for (let ti = 0; ti < target.length && qi < needle.length; ti++) {
    if (target[ti] !== needle[qi]) continue;
    if (ti === lastMatchIdx + 1) consecutive++;
    else if (lastMatchIdx >= 0) totalGap += ti - lastMatchIdx - 1;
    if (ti >= basenameStart) basenameMatches++;
    lastMatchIdx = ti;
    qi++;
  }
  if (qi < needle.length) return null;
  const quality = Math.max(0, totalGap - consecutive * 10 - basenameMatches * 5);
  const lengthPenalty = Math.floor(target.length / 4);
  return quality + lengthPenalty;
}

/** Word-boundary anchor rejects `@` embedded in emails / social handles; trailing `.` stripped before lookup. */
export const AT_MENTION_PATTERN = /(?<=^|\s)@([\p{L}\p{N}_./\\-]+)/gu;

export interface AtMentionExpansion {
  /** The raw `@path` token as it appeared in the text. */
  token: string;
  /** The relative path, as resolved against rootDir. */
  path: string;
  /** True if the content was inlined. False = skipped (reason in `skip`). */
  ok: boolean;
  /** Bytes read (only for ok=true and isDirectory=false). */
  bytes?: number;
  /** True when the mention resolved to a directory (ok=true). Block uses `<directory>` instead of `<file>`. */
  isDirectory?: boolean;
  /** Number of files listed when isDirectory=true. */
  entries?: number;
  /** True iff the directory listing was clipped at maxDirEntries. */
  truncated?: boolean;
  /** Why the mention was skipped. Set when ok=false. */
  skip?: "missing" | "not-file" | "too-large" | "escape" | "read-error";
}

export interface AtMentionOptions {
  /** Max file size in bytes before a mention is skipped. */
  maxBytes?: number;
  /** Cap on entries returned for a `@<dir>` listing. Default {@link DEFAULT_AT_DIR_MAX_ENTRIES}. */
  maxDirEntries?: number;
  fs?: {
    exists: (path: string) => boolean;
    isFile: (path: string) => boolean;
    /** Optional — when omitted, directories are skipped as `not-file`. */
    isDir?: (path: string) => boolean;
    /** Optional — receives the directory's absolute path and the project root, returns relative paths and a truncated flag. */
    listDir?: (
      dirAbs: string,
      root: string,
      max: number,
    ) => { files: string[]; truncated: boolean };
    size: (path: string) => number;
    read: (path: string) => string;
  };
}

export function expandAtMentions(
  text: string,
  rootDir: string,
  opts: AtMentionOptions = {},
): { text: string; expansions: AtMentionExpansion[] } {
  const maxBytes = opts.maxBytes ?? DEFAULT_AT_MENTION_MAX_BYTES;
  const maxDirEntries = Math.max(1, opts.maxDirEntries ?? DEFAULT_AT_DIR_MAX_ENTRIES);
  const fs = opts.fs ?? defaultFs;
  const root = resolve(rootDir);
  // De-dupe by token so `@file.ts` referenced twice inlines once.
  const seen = new Map<string, AtMentionExpansion>();
  const expansions: AtMentionExpansion[] = [];
  const dirListings = new Map<string, string[]>();

  for (const match of text.matchAll(AT_MENTION_PATTERN)) {
    const rawPath = match[1] ?? "";
    // Strip trailing dot (sentence terminator): `@foo.ts.` → `@foo.ts`.
    // Keep internal dots intact. Manual loop instead of `/\.+$/` — the
    // regex is O(n²) on dot-heavy non-matches per CodeQL js/polynomial-redos.
    let cleaned = rawPath;
    while (cleaned.endsWith(".")) cleaned = cleaned.slice(0, -1);
    // Strip a single trailing slash so `@docs/` and `@docs` resolve identically.
    if (cleaned.endsWith("/") || cleaned.endsWith("\\")) cleaned = cleaned.slice(0, -1);
    if (!cleaned) continue;
    const token = `@${cleaned}`;
    if (seen.has(token)) continue;

    const expansion = resolveMention(cleaned, root, maxBytes, maxDirEntries, fs, dirListings);
    seen.set(token, expansion);
    expansions.push(expansion);
  }

  if (expansions.length === 0) return { text, expansions };

  // Build the trailing "Referenced files" block. Keep successful
  // inlines and skipped ones (with their reason) so the model sees
  // both what's here and what's missing.
  const blocks: string[] = [];
  for (const ex of expansions) {
    if (ex.ok && ex.isDirectory) {
      const files = dirListings.get(ex.path) ?? [];
      const truncAttr = ex.truncated ? ' truncated="true"' : "";
      const body = files.length > 0 ? `\n${files.join("\n")}\n` : "\n";
      blocks.push(
        `<directory path="${ex.path}" entries="${ex.entries ?? files.length}"${truncAttr}>${body}</directory>`,
      );
    } else if (ex.ok) {
      const content = readSafe(root, ex.path, fs);
      blocks.push(`<file path="${ex.path}">\n${content}\n</file>`);
    } else {
      blocks.push(`<file path="${ex.path}" skipped="${ex.skip}" />`);
    }
  }
  const augmented = `${text}\n\n[Referenced files]\n${blocks.join("\n\n")}`;
  return { text: augmented, expansions };
}

function resolveMention(
  rawPath: string,
  root: string,
  maxBytes: number,
  maxDirEntries: number,
  fs: NonNullable<AtMentionOptions["fs"]>,
  dirListings: Map<string, string[]>,
): AtMentionExpansion {
  // Reject absolute paths — `@/etc/passwd` should not inline.
  if (isAbsolute(rawPath)) {
    return { token: `@${rawPath}`, path: rawPath, ok: false, skip: "escape" };
  }
  const resolved = resolve(root, rawPath);
  // Sandbox escape: after resolution the path must still be inside root.
  const rel = relative(root, resolved);
  if (rel.startsWith("..") || isAbsolute(rel)) {
    return { token: `@${rawPath}`, path: rawPath, ok: false, skip: "escape" };
  }
  if (!fs.exists(resolved)) {
    return { token: `@${rawPath}`, path: rawPath, ok: false, skip: "missing" };
  }
  if (fs.isFile(resolved)) {
    const size = fs.size(resolved);
    if (size > maxBytes) {
      return { token: `@${rawPath}`, path: rawPath, ok: false, skip: "too-large", bytes: size };
    }
    return { token: `@${rawPath}`, path: rawPath, ok: true, bytes: size };
  }
  // Not a file — try the directory branch. listDir is optional; without it,
  // fall back to the legacy not-file skip so test fixtures don't break.
  if (fs.isDir?.(resolved) && fs.listDir) {
    const { files, truncated } = fs.listDir(resolved, root, maxDirEntries);
    dirListings.set(rawPath, files);
    return {
      token: `@${rawPath}`,
      path: rawPath,
      ok: true,
      isDirectory: true,
      entries: files.length,
      truncated,
    };
  }
  return { token: `@${rawPath}`, path: rawPath, ok: false, skip: "not-file" };
}

function readSafe(root: string, rawPath: string, fs: NonNullable<AtMentionOptions["fs"]>): string {
  const resolved = resolve(root, rawPath);
  try {
    return fs.read(resolved);
  } catch {
    return "(read failed)";
  }
}

const defaultFs: NonNullable<AtMentionOptions["fs"]> = {
  exists: (p) => existsSync(p),
  isFile: (p) => {
    try {
      return statSync(p).isFile();
    } catch {
      return false;
    }
  },
  isDir: (p) => {
    try {
      return statSync(p).isDirectory();
    } catch {
      return false;
    }
  },
  listDir: (dirAbs, root, max) => {
    // Walk from project root and filter to entries under dirAbs so the
    // listing inherits the parent .gitignore layers. Walking dirAbs alone
    // would miss the project-root rules above it.
    const dirRel = relative(root, dirAbs).split(/[\\/]/).join("/");
    const walkCap = Math.max(max * 4, 5000);
    const all = listFilesSync(root, { maxResults: walkCap });
    const prefix = dirRel ? `${dirRel}/` : "";
    const filtered = dirRel ? all.filter((f) => f === dirRel || f.startsWith(prefix)) : all;
    return {
      files: filtered.slice(0, max),
      truncated: filtered.length > max,
    };
  },
  size: (p) => {
    try {
      return statSync(p).size;
    } catch {
      return 0;
    }
  },
  read: (p) => readFileSync(p, "utf8"),
};

export {
  AT_URL_PATTERN,
  DEFAULT_AT_URL_MAX_CHARS,
  expandAtUrls,
  stripUrlTail,
} from "./at-mentions-url.js";
export type { AtUrlExpansion, AtUrlOptions } from "./at-mentions-url.js";
