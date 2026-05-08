/** Native FS tools — sandbox enforced here, not delegated. `edit_file` takes a single SEARCH/REPLACE string. */

import { promises as fs } from "node:fs";
import * as pathMod from "node:path";
import picomatch from "picomatch";
import { DEFAULT_INDEX_EXCLUDES } from "../index/config.js";
import type { ToolRegistry } from "../tools.js";
import { applyEdit, applyMultiEdit } from "./fs/edit.js";
import { globFiles } from "./fs/glob.js";
import { searchContent, searchFiles } from "./fs/search.js";

export { lineDiff } from "./fs/edit.js";

export interface FilesystemToolsOptions {
  /** Absolute directory the tools may read/write. Paths outside this are refused. */
  rootDir: string;
  /** false → register only read-side tools. Default true. */
  allowWriting?: boolean;
  /** Per-read byte cap; floor against OOM on a multi-GB blob. */
  maxReadBytes?: number;
  /** Cap on total bytes from listing/grep tools — bounds tree-as-one-string accidents. */
  maxListBytes?: number;
}

const DEFAULT_MAX_READ_BYTES = 2 * 1024 * 1024;
const DEFAULT_MAX_LIST_BYTES = 256 * 1024;

/** Auto-preview threshold — files above this force the model to scope (range/head/tail). */
const DEFAULT_AUTO_PREVIEW_LINES = 200;
const AUTO_PREVIEW_HEAD_LINES = 80;
const AUTO_PREVIEW_TAIL_LINES = 40;

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

export function registerFilesystemTools(
  registry: ToolRegistry,
  opts: FilesystemToolsOptions,
): ToolRegistry {
  const rootDir = pathMod.resolve(opts.rootDir);
  const allowWriting = opts.allowWriting !== false;
  const maxReadBytes = opts.maxReadBytes ?? DEFAULT_MAX_READ_BYTES;
  const maxListBytes = opts.maxListBytes ?? DEFAULT_MAX_LIST_BYTES;

  /** Resolve path, enforce it's under rootDir, return absolute. */
  const safePath = (raw: unknown): string => {
    if (typeof raw !== "string" || raw.length === 0) {
      throw new Error("path must be a non-empty string");
    }
    // Sandbox-root semantics: a leading POSIX-style `/` (or `\` on
    // Windows) means "from the project root", not "from the filesystem
    // root". Models routinely write `path: "/"` or `path: "/src/foo.ts"`
    // intending the sandbox root — without this normalization,
    // path.resolve interprets `/` as the actual drive root (`F:\` on
    // Windows, `/` on POSIX) and the escape check rightly rejects it,
    // confusing the model. Strip leading separators so the rest of the
    // resolution treats the input as relative to rootDir. Drive-letter
    // absolutes (`C:\foo`) and Unix absolutes outside rootDir still
    // get caught by the relative-escape check below.
    let normalized = raw;
    while (normalized.startsWith("/") || normalized.startsWith("\\")) {
      normalized = normalized.slice(1);
    }
    if (normalized.length === 0) normalized = ".";
    const resolved = pathMod.resolve(rootDir, normalized);
    const normRoot = pathMod.resolve(rootDir);
    // Use relative() to catch any `..` segments that escape.
    const rel = pathMod.relative(normRoot, resolved);
    if (rel.startsWith("..") || pathMod.isAbsolute(rel)) {
      throw new Error(
        `path escapes sandbox root (${normRoot}): ${raw} — workspace is pinned at launch; quit and relaunch with \`reasonix code --dir <path>\` to work in a different folder`,
      );
    }
    return resolved;
  };

  registry.register({
    name: "read_file",
    parallelSafe: true,
    description: `Read a file under the sandbox root. To save context, PREFER to scope the read instead of pulling the whole file:
  - head: N  → first N lines (imports, public API, small configs)
  - tail: N  → last N lines (recently-added code, log tails)
  - range: "A-B"  → inclusive line range A..B, 1-indexed (e.g. "120-180" around an edit site)
When none of these is given AND the file is longer than ${DEFAULT_AUTO_PREVIEW_LINES} lines, the tool auto-returns a head+tail preview with an "N lines omitted" marker rather than dumping everything. If you need the middle, re-call with a range. Prefer search_content to locate a symbol first, then read_file with a range around the hit — one scoped read beats three full-file reads.`,
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
    fn: async (args: { path: string; head?: number; tail?: number; range?: string }) => {
      const abs = safePath(args.path);
      // Open once and reuse the fd so the directory check and the read
      // bind to the same inode — closes the stat→read TOCTOU race.
      const fh = await fs.open(abs, "r");
      let raw: Buffer;
      try {
        const stat = await fh.stat();
        if (stat.isDirectory()) {
          throw new Error(`not a file: ${args.path} (it's a directory)`);
        }
        raw = await fh.readFile();
      } finally {
        await fh.close();
      }
      if (raw.length > maxReadBytes) {
        const headBytes = raw.slice(0, maxReadBytes).toString("utf8");
        return `${headBytes}\n\n[…truncated ${raw.length - maxReadBytes} bytes — file is ${raw.length} B, cap ${maxReadBytes} B. Retry with head/tail/range for targeted view.]`;
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
      // to head/tail / auto-preview instead of erroring.
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

      // No explicit scope + file is small → full content.
      if (totalLines <= DEFAULT_AUTO_PREVIEW_LINES) return lines.join("\n");

      // No explicit scope + file is large → head + tail preview plus
      // a marker telling the model how much it missed and how to get
      // it. This is the single biggest lever on read_file token cost —
      // historically a 500-line file dumped ~4K tokens into the turn
      // even when the model only needed 20 of them.
      const head = lines.slice(0, AUTO_PREVIEW_HEAD_LINES).join("\n");
      const tail = lines.slice(totalLines - AUTO_PREVIEW_TAIL_LINES).join("\n");
      const omitted = totalLines - AUTO_PREVIEW_HEAD_LINES - AUTO_PREVIEW_TAIL_LINES;
      return [
        `[auto-preview: head ${AUTO_PREVIEW_HEAD_LINES} + tail ${AUTO_PREVIEW_TAIL_LINES} of ${totalLines} lines]`,
        head,
        `\n[… ${omitted} lines omitted — call read_file again with range:"A-B" (1-indexed) or head / tail to get the middle]\n`,
        tail,
      ].join("\n");
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
    fn: async (args: { path?: string }) => {
      const abs = safePath(args.path ?? ".");
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
    fn: async (args: { path?: string; maxDepth?: number; include_deps?: boolean }) => {
      const startAbs = safePath(args.path ?? ".");
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
        safePath(args.path ?? "."),
        { ...args, signal: toolCtx?.signal },
      ),
  });

  registry.register({
    name: "search_content",
    parallelSafe: true,
    description:
      "Recursively grep file CONTENTS for a substring or regex. This is the right tool for 'find all places that call X', 'where is Y referenced', 'what files contain Z'. Different from search_files (which matches FILE NAMES). Returns one match per line in 'path:line: text' format. Skips dependency / VCS / build directories (node_modules, .git, dist, build, .next, target, .venv) and binary files by default.",
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
        safePath(args.path ?? "."),
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
      globFiles({ rootDir, skipDirNames: SKIP_DIR_NAMES }, safePath(args.path ?? "."), {
        ...args,
        signal: toolCtx?.signal,
      }),
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
    fn: async (args: { path: string }) => {
      const abs = safePath(args.path);
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
    fn: async (args: { path: string; content: string }) => {
      const abs = safePath(args.path);
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
    fn: async (args: { path: string; search: string; replace: string }) =>
      applyEdit(rootDir, safePath(args.path), args),
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
    fn: async (args: { edits: Array<{ path: string; search: string; replace: string }> }) => {
      const resolved = (args.edits ?? []).map((e) => ({
        abs: safePath(e?.path),
        search: e?.search,
        replace: e?.replace,
      }));
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
    fn: async (args: { path: string }) => {
      const abs = safePath(args.path);
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
    fn: async (args: { source: string; destination: string }) => {
      const src = safePath(args.source);
      const dst = safePath(args.destination);
      await fs.mkdir(pathMod.dirname(dst), { recursive: true });
      await fs.rename(src, dst);
      return `moved ${displayRel(rootDir, src)} → ${displayRel(rootDir, dst)}`;
    },
  });

  return registry;
}
