/** Native FS tools — sandbox enforced here, not delegated. `edit_file` takes a single SEARCH/REPLACE string. */

import { promises as fs } from "node:fs";
import * as pathMod from "node:path";
import picomatch from "picomatch";
import { addProjectPathAllowed, loadProjectPathAllowed } from "../config.js";
import { type ConfirmationChoice, pauseGate as defaultPauseGate } from "../core/pause-gate.js";
import { DEFAULT_INDEX_EXCLUDES } from "../index/config.js";
import type { ToolCallContext, ToolRegistry } from "../tools.js";
import { applyEdit, applyMultiEdit } from "./fs/edit.js";
import { globFiles } from "./fs/glob.js";
import { extractOutline, formatOutline } from "./fs/outline.js";
import { searchContent, searchFiles } from "./fs/search.js";

export { lineDiff } from "./fs/edit.js";

export interface FilesystemToolsOptions {
  /** Absolute directory the tools may read/write. Paths outside this are refused. */
  rootDir: string;
  /** false → register only read-side tools. Default true. */
  allowWriting?: boolean;
  /** Files at or under this size get full content; larger go to outline mode. Default 512 KiB. */
  outlineThresholdBytes?: number;
  /** Cap on total bytes from listing/grep tools — bounds tree-as-one-string accidents. */
  maxListBytes?: number;
}

const DEFAULT_OUTLINE_THRESHOLD_BYTES = 512 * 1024;
const DEFAULT_MAX_LIST_BYTES = 256 * 1024;

/** Refuse load above this; outline-mode would have to slurp the whole file to scan it. */
const HARD_MAX_FILE_BYTES = 32 * 1024 * 1024;

/** Lines shown for orientation when a file is too big for full content. */
const OUTLINE_HEAD_LINES = 80;

/** Skipped unless `include_deps:true` — shared with the semantic indexer via DEFAULT_INDEX_EXCLUDES. */
const SKIP_DIR_NAMES: ReadonlySet<string> = new Set(DEFAULT_INDEX_EXCLUDES.dirs);

/** First line of binary defense; NUL-byte sniff is the second (catches mislabeled `.txt`). */
const BINARY_EXTENSIONS: ReadonlySet<string> = new Set(DEFAULT_INDEX_EXCLUDES.exts);

export function displayRel(rootDir: string, full: string): string {
  return pathMod.relative(rootDir, full).replaceAll("\\", "/");
}

const GLOB_METACHARS = /[*?{[]/;

/** Glob via picomatch when metachars present, else case-insensitive substring — keeps `.ts` / `test` callers working. Slash in pattern → match rel-path; otherwise basename. */
export function compileNameFilter(
  filter: string | null | undefined,
): ((name: string, rel: string) => boolean) | null {
  if (!filter) return null;
  if (!GLOB_METACHARS.test(filter)) {
    const needle = filter.toLowerCase();
    return (name) => name.toLowerCase().includes(needle);
  }
  const matchPath = filter.includes("/");
  const isMatch = picomatch(filter, { dot: true, nocase: true });
  return matchPath ? (_n, rel) => isMatch(rel) : (name) => isMatch(name);
}

function isLikelyBinaryByName(name: string): boolean {
  const dot = name.lastIndexOf(".");
  if (dot < 0) return false;
  return BINARY_EXTENSIONS.has(name.slice(dot).toLowerCase());
}

/** Sniff first 8 KiB for a NUL byte — catches binary files whose extension lied. UTF-16 (rare in source) is an accepted false positive. */
function looksBinary(buf: Buffer): boolean {
  const end = Math.min(buf.length, 8192);
  for (let i = 0; i < end; i++) {
    if (buf[i] === 0) return true;
  }
  return false;
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KiB`;
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MiB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(2)} GiB`;
}

export function registerFilesystemTools(
  registry: ToolRegistry,
  opts: FilesystemToolsOptions,
): ToolRegistry {
  const rootDir = pathMod.resolve(opts.rootDir);
  const allowWriting = opts.allowWriting !== false;
  const outlineThresholdBytes = opts.outlineThresholdBytes ?? DEFAULT_OUTLINE_THRESHOLD_BYTES;
  const maxListBytes = opts.maxListBytes ?? DEFAULT_MAX_LIST_BYTES;

  const normRoot = pathMod.resolve(rootDir);
  /** Approved-this-session directory prefixes — `run_once` keeps the user from being asked twice for follow-up reads in the same dir. Wiped on process exit, not persisted. */
  const sessionApproved = new Set<string>();
  /** In-flight gate prompts keyed by `allowPrefix` so parallel reads under the same dir only fire one modal. */
  const inflightGate = new Map<string, Promise<ConfirmationChoice>>();

  function pathIsUnder(child: string, parent: string): boolean {
    const rel = pathMod.relative(parent, child);
    return rel === "" || (!rel.startsWith("..") && !pathMod.isAbsolute(rel));
  }

  /** Heuristic: does the raw input express "I really mean this absolute system path" rather than the model-convention sandbox-relative form? Windows drive-letter prefixes always count; POSIX absolutes only count when their first segment is a known system root. */
  function looksLikeAbsoluteSystemPath(raw: string): boolean {
    if (/^[A-Za-z]:[\\/]/.test(raw)) return true;
    return /^\/(?:home|Users|etc|var|opt|tmp|usr|mnt|Library|Volumes|proc|sys|dev|run|srv|media|Applications|System|root|boot|private)(?:[/\\]|$)/.test(
      raw,
    );
  }

  async function ensureOutsideSandboxAllowed(
    abs: string,
    intent: "read" | "write",
    toolName: string,
    ctx: ToolCallContext | undefined,
  ): Promise<void> {
    for (const dir of loadProjectPathAllowed(rootDir)) {
      if (pathIsUnder(abs, dir)) return;
    }
    for (const dir of sessionApproved) {
      if (pathIsUnder(abs, dir)) return;
    }
    const stat = await safeLstat(abs);
    const allowPrefix = stat?.isDirectory() ? abs : pathMod.dirname(abs);
    let pending = inflightGate.get(allowPrefix);
    if (!pending) {
      const gate = ctx?.confirmationGate ?? defaultPauseGate;
      pending = gate.ask({
        kind: "path_access",
        payload: { path: abs, intent, toolName, sandboxRoot: normRoot, allowPrefix },
      });
      inflightGate.set(allowPrefix, pending);
      void pending.finally(() => inflightGate.delete(allowPrefix));
    }
    const choice = await pending;
    if (choice.type === "deny") {
      throw new Error(
        `user denied access to ${abs}${choice.denyContext ? ` — ${choice.denyContext}` : ""}`,
      );
    }
    if (choice.type === "always_allow") {
      addProjectPathAllowed(rootDir, choice.prefix);
    } else {
      sessionApproved.add(allowPrefix);
    }
  }

  /** Resolve path, route outside-sandbox access through the approval gate, return absolute. */
  const safePath = async (
    raw: unknown,
    toolName: string,
    ctx: ToolCallContext | undefined,
    intent: "read" | "write" = "read",
  ): Promise<string> => {
    if (typeof raw !== "string" || raw.length === 0) {
      throw new Error("path must be a non-empty string");
    }
    if (looksLikeAbsoluteSystemPath(raw)) {
      const abs = pathMod.resolve(raw);
      if (pathIsUnder(abs, normRoot)) return abs;
      await ensureOutsideSandboxAllowed(abs, intent, toolName, ctx);
      return abs;
    }
    // Sandbox-root semantics: leading `/` or `\` means "from project root", not "from filesystem root".
    // Model routinely writes `path: "/src/foo.ts"` intending rootDir-relative.
    let normalized = raw;
    while (normalized.startsWith("/") || normalized.startsWith("\\")) {
      normalized = normalized.slice(1);
    }
    if (normalized.length === 0) normalized = ".";
    const resolved = pathMod.resolve(rootDir, normalized);
    if (!pathIsUnder(resolved, normRoot)) {
      throw new Error(
        `path escapes sandbox root (${normRoot}): ${raw} — use an absolute system path like /Users/foo or C:\\Users\\foo to request approved outside-sandbox access`,
      );
    }
    return resolved;
  };

  /** lstat that swallows ENOENT so we can still gate writes to brand-new paths. */
  async function safeLstat(p: string): Promise<import("node:fs").Stats | null> {
    try {
      return await fs.lstat(p);
    } catch {
      return null;
    }
  }

  registry.register({
    name: "read_file",
    parallelSafe: true,
    description: `Read a file under the sandbox root. Default behaviour returns FULL CONTENT for files at or under ${Math.round(DEFAULT_OUTLINE_THRESHOLD_BYTES / 1024)} KiB — trust the prompt cache, don't pre-truncate. Optional scoping:
  - head: N  → first N lines (cheap probe of imports / config head)
  - tail: N  → last N lines (recent-tail of a log)
  - range: "A-B"  → inclusive 1-indexed range (e.g. "120-180" around an edit site)
Files OVER the threshold auto-switch to outline mode: file metadata + first ${OUTLINE_HEAD_LINES} lines + a top-level symbol outline (TS/JS exports, Python def/class, Go func/type, Rust fn/struct/impl/trait, Markdown headings, Protobuf message/service/rpc, plain-text chapter markers) + concrete next-step commands. No middle bytes — drill in with range / search_content. Files over ${Math.round(HARD_MAX_FILE_BYTES / (1024 * 1024))} MiB are refused entirely (use grep / range). Binary files are refused — use get_file_info if you only need stat.`,
    readOnly: true,
    stormExempt: true,
    parameters: {
      type: "object",
      properties: {
        path: { type: "string", description: "Path to read (relative to rootDir or absolute)." },
        head: { type: "integer", description: "If set, return only the first N lines." },
        tail: { type: "integer", description: "If set, return only the last N lines." },
        range: {
          type: "string",
          description:
            'Inclusive line range like "50-100" or "50-50". 1-indexed. Takes precedence over head/tail when all three are set. Out-of-range requests clamp to file bounds.',
        },
      },
      required: ["path"],
    },
    fn: async (
      args: { path: string; head?: number; tail?: number; range?: string },
      ctx?: ToolCallContext,
    ) => {
      const abs = await safePath(args.path, "read_file", ctx);
      const rel = displayRel(rootDir, abs);
      // Open once and reuse the fd so the directory check and the read
      // bind to the same inode — closes the stat→read TOCTOU race.
      const fh = await fs.open(abs, "r");
      let raw: Buffer;
      let sizeBytes: number;
      try {
        const stat = await fh.stat();
        if (stat.isDirectory()) {
          throw new Error(`not a file: ${args.path} (it's a directory)`);
        }
        sizeBytes = stat.size;
        if (sizeBytes > HARD_MAX_FILE_BYTES) {
          return [
            `[refused: ${rel} is ${formatBytes(sizeBytes)} (> ${formatBytes(HARD_MAX_FILE_BYTES)} hard ceiling) — too large to load]`,
            "Use one of:",
            `  - search_content path:"${rel}" pattern:"<your regex>"  — grep within the file`,
            `  - read_file path:"${rel}" range:"A-B"                   — read a specific 1-indexed line range`,
            `  - read_file path:"${rel}" head:N  /  tail:N             — read N lines at the start or end`,
          ].join("\n");
        }
        raw = await fh.readFile();
      } finally {
        await fh.close();
      }

      if (looksBinary(raw)) {
        return `[refused: ${rel} appears to be binary (${formatBytes(sizeBytes)}) — read_file returns text only. Use get_file_info for stat.]`;
      }

      const text = raw.toString("utf8");
      let lines = text.split(/\r?\n/);
      // Most files end with '\n' which splits into an empty trailing
      // entry; drop it so head/tail/range counts match the user's
      // visible line numbers in an editor.
      if (lines.length > 0 && lines[lines.length - 1] === "") lines = lines.slice(0, -1);
      const totalLines = lines.length;

      // range wins over head/tail when set — the most precise ask
      // should dominate. Parse "A-B" strictly; bad formats fall through
      // to head/tail / outline-mode instead of erroring.
      if (typeof args.range === "string" && /^\d+\s*-\s*\d+$/.test(args.range)) {
        const [rawStart, rawEnd] = args.range.split("-").map((s) => Number.parseInt(s, 10));
        const start = Math.max(1, rawStart ?? 1);
        const end = Math.min(totalLines, Math.max(start, rawEnd ?? totalLines));
        const slice = lines.slice(start - 1, end);
        const label = `[range ${start}-${end} of ${totalLines} lines]`;
        return `${label}\n${slice.join("\n")}`;
      }
      if (typeof args.head === "number" && args.head > 0) {
        const count = Math.min(args.head, totalLines);
        const slice = lines.slice(0, count);
        const marker =
          count < totalLines
            ? `\n\n[…head ${count} of ${totalLines} lines — call again with range / tail for more]`
            : "";
        return slice.join("\n") + marker;
      }
      if (typeof args.tail === "number" && args.tail > 0) {
        const count = Math.min(args.tail, totalLines);
        const slice = lines.slice(totalLines - count);
        const marker =
          count < totalLines
            ? `[…tail ${count} of ${totalLines} lines — call again with range / head for more]\n\n`
            : "";
        return marker + slice.join("\n");
      }

      // No explicit scope + file fits the threshold → full content.
      // Trust the prompt cache: a 100K-token file read once amortizes
      // across every turn of the same conversation.
      if (sizeBytes <= outlineThresholdBytes) return lines.join("\n");

      // No explicit scope + file is over the threshold → outline mode.
      // Return enough for the model to orient (head + symbol map) plus
      // concrete next-step commands. Avoids dumping a 5 MB proto into
      // every cached prefix while still surfacing what's inside.
      const head = lines.slice(0, Math.min(OUTLINE_HEAD_LINES, totalLines)).join("\n");
      const outline = formatOutline(extractOutline(abs, lines));
      const parts: string[] = [
        `[large file: ${formatBytes(sizeBytes)}, ${totalLines} lines — outline mode (threshold ${formatBytes(outlineThresholdBytes)})]`,
        "",
        `[head ${Math.min(OUTLINE_HEAD_LINES, totalLines)} lines for orientation]`,
        head,
      ];
      if (outline) parts.push("", outline);
      parts.push(
        "",
        "[to read more, call one of:",
        `  - read_file path:"${rel}" range:"A-B"          — 1-indexed line range`,
        `  - read_file path:"${rel}" head:N  /  tail:N    — first/last N lines`,
        `  - search_content path:"${rel}" pattern:"..."   — grep within this file]`,
      );
      return parts.join("\n");
    },
  });

  registry.register({
    name: "list_directory",
    parallelSafe: true,
    description:
      "List entries in a directory under the sandbox root. Returns one line per entry, marking directories with a trailing slash. Not recursive — use directory_tree for that.",
    readOnly: true,
    stormExempt: true,
    parameters: {
      type: "object",
      properties: {
        path: { type: "string", description: "Directory to list (default: root)." },
      },
    },
    fn: async (args: { path?: string }, ctx?: ToolCallContext) => {
      const abs = await safePath(args.path ?? ".", "list_directory", ctx);
      const entries = await fs.readdir(abs, { withFileTypes: true });
      const lines: string[] = [];
      for (const e of entries.sort((a, b) => a.name.localeCompare(b.name))) {
        lines.push(e.isDirectory() ? `${e.name}/` : e.name);
      }
      return lines.join("\n") || "(empty directory)";
    },
  });

  registry.register({
    name: "directory_tree",
    parallelSafe: true,
    description: `Recursively list entries in a directory. Shows indented tree structure with directories marked '/'. Budget-aware by default:
  - maxDepth defaults to 2 (root + one level). A depth-4 tree on a real repo blew ~5K tokens in one call. If you truly need deeper, pass maxDepth:N explicitly.
  - Skips ${[...SKIP_DIR_NAMES].sort().join(", ")} unless include_deps:true. Traversing into node_modules / .git / dist is almost always token-waste.
  - Large subtrees (>50 children) auto-collapse to "[N files, M dirs hidden — list_directory <path> to inspect]" so one huge folder can't dominate the output.
Prefer \`list_directory\` for a single-level view, \`search_files\` to find specific paths, and \`search_content\` to find code.`,
    readOnly: true,
    parameters: {
      type: "object",
      properties: {
        path: { type: "string", description: "Root of the tree (default: sandbox root)." },
        maxDepth: {
          type: "integer",
          description:
            "Max recursion depth (default 2). Depth 0 shows only the top-level entries; depth 2 is usually enough to see module structure.",
        },
        include_deps: {
          type: "boolean",
          description:
            "When true, also traverse node_modules / .git / dist / build / etc. Off by default — most exploration questions are about the user's own code.",
        },
      },
    },
    fn: async (
      args: { path?: string; maxDepth?: number; include_deps?: boolean },
      ctx?: ToolCallContext,
    ) => {
      const startAbs = await safePath(args.path ?? ".", "directory_tree", ctx);
      const maxDepth = typeof args.maxDepth === "number" ? args.maxDepth : 2;
      const includeDeps = args.include_deps === true;
      const lines: string[] = [];
      let totalBytes = 0;
      let truncated = false;
      // Per-directory child cap — long fixture / asset folders (200+
      // snapshots) would otherwise dominate; the collapse keeps the
      // overall shape visible. Modest: normal source dirs have <50
      // entries.
      const PER_DIR_CHILD_CAP = 50;
      const walk = async (dir: string, depth: number): Promise<void> => {
        if (truncated) return;
        if (depth > maxDepth) return;
        let entries: import("node:fs").Dirent[];
        try {
          entries = await fs.readdir(dir, { withFileTypes: true });
        } catch {
          return;
        }
        entries.sort((a, b) => a.name.localeCompare(b.name));
        let emitted = 0;
        for (const e of entries) {
          if (truncated) return;
          // Dep-skip applies only to DIRECTORIES (a file named
          // "node_modules" is fine to list). Anything in the skip set
          // still shows up as a single node with a trailing " (skipped)"
          // hint so the model knows the dir exists but wasn't walked.
          const skip = e.isDirectory() && !includeDeps && SKIP_DIR_NAMES.has(e.name);
          if (emitted >= PER_DIR_CHILD_CAP) {
            const remaining = entries.length - emitted;
            let restFiles = 0;
            let restDirs = 0;
            for (const r of entries.slice(emitted)) {
              if (r.isDirectory()) restDirs++;
              else restFiles++;
            }
            const indent = "  ".repeat(depth);
            lines.push(
              `${indent}[… ${remaining} entries hidden (${restDirs} dirs, ${restFiles} files) — list_directory on this path to see all]`,
            );
            return;
          }
          const indent = "  ".repeat(depth);
          const suffix = skip ? " (skipped — pass include_deps:true to traverse)" : "";
          const line = e.isDirectory() ? `${indent}${e.name}/${suffix}` : `${indent}${e.name}`;
          totalBytes += line.length + 1;
          if (totalBytes > maxListBytes) {
            lines.push(`  [… tree truncated at ${maxListBytes} bytes …]`);
            truncated = true;
            return;
          }
          lines.push(line);
          emitted++;
          if (e.isDirectory() && !skip) {
            await walk(pathMod.join(dir, e.name), depth + 1);
          }
        }
      };
      await walk(startAbs, 0);
      return lines.join("\n") || "(empty tree)";
    },
  });

  registry.register({
    name: "search_files",
    parallelSafe: true,
    description:
      "Find files whose NAME matches a substring or regex. Case-insensitive. Walks the directory recursively under the sandbox root. Returns one path per line. Skips dependency / VCS / build directories (node_modules, .git, dist, build, .next, target, .venv) by default.",
    readOnly: true,
    parameters: {
      type: "object",
      properties: {
        path: { type: "string", description: "Directory to start the search at (default: root)." },
        pattern: {
          type: "string",
          description: "Substring (or regex) to match against filenames.",
        },
        include_deps: {
          type: "boolean",
          description:
            "When true, also walk node_modules / .git / dist / build / etc. Off by default — most filename searches are about the user's own code.",
        },
      },
      required: ["pattern"],
    },
    fn: async (args: { path?: string; pattern: string; include_deps?: boolean }, toolCtx) =>
      searchFiles(
        { rootDir, maxListBytes, skipDirNames: SKIP_DIR_NAMES },
        await safePath(args.path ?? ".", "search_files", toolCtx),
        { ...args, signal: toolCtx?.signal },
      ),
  });

  registry.register({
    name: "search_content",
    parallelSafe: true,
    description:
      "Recursively grep file CONTENTS for a substring or regex. This is the right tool for 'find all places that call X', 'where is Y referenced', 'what files contain Z'. Different from search_files (which matches FILE NAMES). Returns one match per line in 'path:line: text' format. Per-file hits are capped at 30 (a footer reports any extras); when the byte budget is mostly spent the remaining files switch to a 'rel: N matches' histogram so distribution stays visible instead of one popular file drowning the rest. Pass `summary_only:true` to skip line content entirely and get just the histogram. Skips dependency / VCS / build directories (node_modules, .git, dist, build, .next, target, .venv) and binary files by default.",
    readOnly: true,
    parameters: {
      type: "object",
      properties: {
        pattern: {
          type: "string",
          description: "Substring (or regex) to search file contents for.",
        },
        path: {
          type: "string",
          description: "Directory to start the search at (default: sandbox root).",
        },
        glob: {
          type: "string",
          description:
            "Optional filename filter. Real glob when the value contains `*`, `?`, `{`, or `[` — e.g. '*.ts', '**/*.tsx', 'src/**/*.{ts,tsx}'. Plain substring otherwise — e.g. '.ts' (suffix), 'test' (anywhere in the name). Patterns containing `/` match against the path relative to the search root; otherwise just the basename.",
        },
        case_sensitive: {
          type: "boolean",
          description: "When true, match case exactly. Default false (case-insensitive).",
        },
        include_deps: {
          type: "boolean",
          description:
            "When true, also search inside node_modules / .git / dist / build / etc. Off by default — most exploration questions are about the user's own code.",
        },
        context: {
          type: "integer",
          description:
            "Lines of context to show around each match (both before and after). Default 0 (just the matching line). Capped at 20. Output uses ripgrep style: `:` after the line number on the matching line, `-` on context lines, `--` separating non-adjacent windows.",
        },
        summary_only: {
          type: "boolean",
          description:
            "When true, skip line content and return one 'rel: N matches' line per matching file. Use for 'where does this exist at all' questions before drilling in with a targeted read_file.",
        },
      },
      required: ["pattern"],
    },
    fn: async (
      args: {
        pattern: string;
        path?: string;
        glob?: string;
        case_sensitive?: boolean;
        include_deps?: boolean;
        context?: number;
        summary_only?: boolean;
      },
      toolCtx,
    ) =>
      searchContent(
        {
          rootDir,
          maxListBytes,
          skipDirNames: SKIP_DIR_NAMES,
          isBinaryByName: isLikelyBinaryByName,
          nameMatch: compileNameFilter(typeof args.glob === "string" ? args.glob : null),
        },
        await safePath(args.path ?? ".", "search_content", toolCtx),
        { ...args, signal: toolCtx?.signal },
      ),
  });

  registry.register({
    name: "glob",
    parallelSafe: true,
    description:
      "List files matching a glob pattern, sorted by mtime (most-recently-modified first) by default. Use this for 'what changed lately', 'find all *.test.ts', 'all configs under src/'. Glob syntax matches the cross-tool standard: `*` (any chars in one segment), `**` (any segments), `?` (one char), `{a,b}` (alternation). Pattern matches against the path RELATIVE to the search root (e.g. 'src/**/*.ts' from project root). Skips node_modules / .git / dist / build / etc by default. Default limit 200; raise via `limit` (max 1000). Different from `search_files` (substring on basename) and `search_content` (matches inside file contents).",
    readOnly: true,
    parameters: {
      type: "object",
      properties: {
        pattern: {
          type: "string",
          description: "Glob pattern, e.g. 'src/**/*.ts', '**/*.{md,mdx}', 'tests/*.test.ts'.",
        },
        path: {
          type: "string",
          description:
            "Base directory to walk (default: sandbox root). The pattern matches relative to this path.",
        },
        sort_by: {
          type: "string",
          enum: ["mtime", "name"],
          description:
            "Sort order. 'mtime' (default) shows most-recently-modified first — useful for 'what did I change today'. 'name' is alphabetical.",
        },
        include_deps: {
          type: "boolean",
          description:
            "When true, also walk node_modules / .git / dist / build / etc. Off by default.",
        },
        limit: {
          type: "integer",
          description: "Cap on returned matches. Default 200; clamped to [1, 1000].",
        },
      },
      required: ["pattern"],
    },
    fn: async (
      args: {
        pattern: string;
        path?: string;
        sort_by?: "mtime" | "name";
        include_deps?: boolean;
        limit?: number;
      },
      toolCtx,
    ) =>
      globFiles(
        { rootDir, skipDirNames: SKIP_DIR_NAMES },
        await safePath(args.path ?? ".", "glob", toolCtx),
        { ...args, signal: toolCtx?.signal },
      ),
  });

  registry.register({
    name: "get_file_info",
    parallelSafe: true,
    description:
      "Stat a path under the sandbox root. Returns type (file|directory|symlink), size in bytes, mtime in ISO-8601.",
    readOnly: true,
    parameters: {
      type: "object",
      properties: {
        path: { type: "string" },
      },
      required: ["path"],
    },
    fn: async (args: { path: string }, ctx?: ToolCallContext) => {
      const abs = await safePath(args.path, "get_file_info", ctx);
      const st = await fs.lstat(abs);
      const type = st.isDirectory() ? "directory" : st.isSymbolicLink() ? "symlink" : "file";
      return JSON.stringify({
        type,
        size: st.size,
        mtime: st.mtime.toISOString(),
      });
    },
  });

  if (!allowWriting) return registry;

  registry.register({
    name: "write_file",
    description:
      "Create or overwrite a file under the sandbox root with the given content. Parent directories are created as needed.",
    parameters: {
      type: "object",
      properties: {
        path: { type: "string" },
        content: { type: "string" },
      },
      required: ["path", "content"],
    },
    fn: async (args: { path: string; content: string }, ctx?: ToolCallContext) => {
      const abs = await safePath(args.path, "write_file", ctx, "write");
      await fs.mkdir(pathMod.dirname(abs), { recursive: true });
      await fs.writeFile(abs, args.content, "utf8");
      return `wrote ${args.content.length} chars to ${displayRel(rootDir, abs)}`;
    },
  });

  registry.register({
    name: "edit_file",
    description:
      "Apply a SEARCH/REPLACE edit to an existing file. `search` must match exactly (whitespace sensitive) — no regex. The match must be unique in the file; otherwise the edit is refused to avoid surprise rewrites.",
    parameters: {
      type: "object",
      properties: {
        path: { type: "string" },
        search: { type: "string", description: "Exact text to find (must be unique)." },
        replace: { type: "string", description: "Text to substitute in place of `search`." },
      },
      required: ["path", "search", "replace"],
    },
    fn: async (args: { path: string; search: string; replace: string }, ctx?: ToolCallContext) =>
      applyEdit(rootDir, await safePath(args.path, "edit_file", ctx, "write"), args),
  });

  registry.register({
    name: "multi_edit",
    description:
      "Apply N SEARCH/REPLACE edits across ONE OR MORE files in a single atomic call. Edits run sequentially in array order; for edits that touch the same file, a later edit can match text inserted by an earlier one. If ANY edit fails (search not found, ambiguous match, empty search, file unreadable), NO files are written — atomic at the validation layer. Same per-edit rules as edit_file: `search` is exact text (whitespace sensitive, no regex) and must be unique in its target file at the moment that edit applies. Use this for renames spanning multiple files, cross-file refactors, or any batch where you'd otherwise loop edit_file.",
    parameters: {
      type: "object",
      properties: {
        edits: {
          type: "array",
          description: "Edits to apply in order. Length ≥ 1. Each edit names its own target file.",
          items: {
            type: "object",
            properties: {
              path: {
                type: "string",
                description: "File the edit targets (sandbox-relative or absolute).",
              },
              search: {
                type: "string",
                description: "Exact text to find (must be unique in the file).",
              },
              replace: { type: "string", description: "Text to substitute in place of `search`." },
            },
            required: ["path", "search", "replace"],
          },
        },
      },
      required: ["edits"],
    },
    fn: async (
      args: { edits: Array<{ path: string; search: string; replace: string }> },
      ctx?: ToolCallContext,
    ) => {
      const resolved = await Promise.all(
        (args.edits ?? []).map(async (e) => ({
          abs: await safePath(e?.path, "multi_edit", ctx, "write"),
          search: e?.search,
          replace: e?.replace,
        })),
      );
      return applyMultiEdit(rootDir, resolved);
    },
  });

  registry.register({
    name: "create_directory",
    description: "Create a directory (and any missing parents) under the sandbox root.",
    parameters: {
      type: "object",
      properties: { path: { type: "string" } },
      required: ["path"],
    },
    fn: async (args: { path: string }, ctx?: ToolCallContext) => {
      const abs = await safePath(args.path, "create_directory", ctx, "write");
      await fs.mkdir(abs, { recursive: true });
      return `created ${displayRel(rootDir, abs)}/`;
    },
  });

  registry.register({
    name: "move_file",
    description: "Rename/move a file or directory under the sandbox root.",
    parameters: {
      type: "object",
      properties: {
        source: { type: "string" },
        destination: { type: "string" },
      },
      required: ["source", "destination"],
    },
    fn: async (args: { source: string; destination: string }, ctx?: ToolCallContext) => {
      const src = await safePath(args.source, "move_file", ctx, "write");
      const dst = await safePath(args.destination, "move_file", ctx, "write");
      await fs.mkdir(pathMod.dirname(dst), { recursive: true });
      await fs.rename(src, dst);
      return `moved ${displayRel(rootDir, src)} → ${displayRel(rootDir, dst)}`;
    },
  });

  registry.register({
    name: "delete_file",
    description:
      "Delete one file under the sandbox root. Refuses directories — use delete_directory for those. Errors if the path doesn't exist.",
    parameters: {
      type: "object",
      properties: { path: { type: "string" } },
      required: ["path"],
    },
    fn: async (args: { path: string }, ctx?: ToolCallContext) => {
      const abs = await safePath(args.path, "delete_file", ctx, "write");
      const st = await fs.lstat(abs);
      if (st.isDirectory()) {
        throw new Error(
          `delete_file: ${args.path} is a directory — use delete_directory to remove it`,
        );
      }
      await fs.unlink(abs);
      return `deleted ${displayRel(rootDir, abs)}`;
    },
  });

  registry.register({
    name: "delete_directory",
    description:
      "Recursively delete a directory under the sandbox root. Pass `recursive:false` to refuse non-empty directories. Errors if the path doesn't exist.",
    parameters: {
      type: "object",
      properties: {
        path: { type: "string" },
        recursive: {
          type: "boolean",
          description:
            "When true (default) deletes the directory and all its contents. When false, only removes empty directories — non-empty refuses with an error.",
        },
      },
      required: ["path"],
    },
    fn: async (args: { path: string; recursive?: boolean }, ctx?: ToolCallContext) => {
      const abs = await safePath(args.path, "delete_directory", ctx, "write");
      const st = await fs.lstat(abs);
      if (!st.isDirectory()) {
        throw new Error(`delete_directory: ${args.path} is a file — use delete_file to remove it`);
      }
      const recursive = args.recursive !== false;
      // `fs.rm({recursive:false})` rejects every directory regardless of contents;
      // `fs.rmdir` is the empty-only variant we want when the caller said no recursion.
      if (recursive) {
        await fs.rm(abs, { recursive: true, force: false });
      } else {
        await fs.rmdir(abs);
      }
      return `deleted ${displayRel(rootDir, abs)}/${recursive ? " (recursive)" : ""}`;
    },
  });

  registry.register({
    name: "copy_file",
    description:
      "Copy a file or directory under the sandbox root. Both source and destination resolve under the sandbox. Parent directories of the destination are created as needed. Refuses to overwrite an existing destination — delete it first if you want to replace it.",
    parameters: {
      type: "object",
      properties: {
        source: { type: "string" },
        destination: { type: "string" },
      },
      required: ["source", "destination"],
    },
    fn: async (args: { source: string; destination: string }, ctx?: ToolCallContext) => {
      const src = await safePath(args.source, "copy_file", ctx);
      const dst = await safePath(args.destination, "copy_file", ctx, "write");
      await fs.mkdir(pathMod.dirname(dst), { recursive: true });
      await fs.cp(src, dst, { recursive: true, force: false, errorOnExist: true });
      return `copied ${displayRel(rootDir, src)} → ${displayRel(rootDir, dst)}`;
    },
  });

  return registry;
}
