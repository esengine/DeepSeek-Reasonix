import { promises as fs } from "node:fs";
import * as pathMod from "node:path";

export interface SearchContext {
  rootDir: string;
  maxListBytes: number;
  skipDirNames: ReadonlySet<string>;
  isBinaryByName: (name: string) => boolean;
  /** Pre-baked filename→regex/substring matcher; null when no glob filter. */
  nameMatch: ((name: string, rel: string) => boolean) | null;
}

function displayRel(rootDir: string, full: string): string {
  return pathMod.relative(rootDir, full).replaceAll("\\", "/");
}

export async function searchFiles(
  ctx: Pick<SearchContext, "rootDir" | "maxListBytes" | "skipDirNames">,
  startAbs: string,
  args: { pattern: string; include_deps?: boolean },
): Promise<string> {
  const needle = args.pattern.toLowerCase();
  const includeDeps = args.include_deps === true;
  let re: RegExp | null = null;
  try {
    re = new RegExp(args.pattern, "i");
  } catch {
    re = null;
  }
  const matches: string[] = [];
  let totalBytes = 0;
  const walk = async (dir: string): Promise<void> => {
    let entries: import("node:fs").Dirent[];
    try {
      entries = await fs.readdir(dir, { withFileTypes: true });
    } catch {
      return;
    }
    for (const e of entries) {
      const full = pathMod.join(dir, e.name);
      const lower = e.name.toLowerCase();
      const hit = re ? re.test(e.name) : lower.includes(needle);
      if (hit) {
        const rel = displayRel(ctx.rootDir, full);
        if (totalBytes + rel.length + 1 > ctx.maxListBytes) {
          matches.push("[… search truncated — refine pattern …]");
          return;
        }
        matches.push(rel);
        totalBytes += rel.length + 1;
      }
      if (e.isDirectory()) {
        if (!includeDeps && ctx.skipDirNames.has(e.name)) continue;
        await walk(full);
      }
    }
  };
  await walk(startAbs);
  return matches.length === 0 ? "(no matches)" : matches.join("\n");
}

export async function searchContent(
  ctx: SearchContext,
  startAbs: string,
  args: {
    pattern: string;
    case_sensitive?: boolean;
    include_deps?: boolean;
  },
): Promise<string> {
  const caseSensitive = args.case_sensitive === true;
  const includeDeps = args.include_deps === true;
  // Try the pattern as a regex first (lets the model say `\bdispatch\(`
  // for a word-bounded match); fall back to literal substring on
  // invalid regex. No `g` flag — we test once per line, so global
  // statefulness (lastIndex tracking) would just be noise.
  let re: RegExp | null = null;
  try {
    re = new RegExp(args.pattern, caseSensitive ? "" : "i");
  } catch {
    re = null;
  }
  const needle = caseSensitive ? args.pattern : args.pattern.toLowerCase();
  const matches: string[] = [];
  let totalBytes = 0;
  let scanned = 0;
  let truncated = false;

  const walk = async (dir: string): Promise<void> => {
    if (truncated) return;
    let entries: import("node:fs").Dirent[];
    try {
      entries = await fs.readdir(dir, { withFileTypes: true });
    } catch {
      return;
    }
    for (const e of entries) {
      if (truncated) return;
      if (e.isDirectory()) {
        if (!includeDeps && ctx.skipDirNames.has(e.name)) continue;
        await walk(pathMod.join(dir, e.name));
        continue;
      }
      if (!e.isFile()) continue;
      const full = pathMod.join(dir, e.name);
      if (ctx.nameMatch && !ctx.nameMatch(e.name, displayRel(ctx.rootDir, full))) continue;
      if (ctx.isBinaryByName(e.name)) continue;
      // Open once and reuse the fd so the size check and read bind to the
      // same inode — avoids the stat→readFile TOCTOU race CodeQL flags.
      let fh: import("node:fs/promises").FileHandle;
      try {
        fh = await fs.open(full, "r");
      } catch {
        continue;
      }
      let raw: Buffer;
      try {
        const st = await fh.stat();
        // Per-file size cap so a 50MB log doesn't dominate the search.
        // Anything legitimately interesting fits in 2 MB; bigger files
        // are usually data dumps or generated bundles.
        if (st.size > 2 * 1024 * 1024) {
          await fh.close();
          continue;
        }
        raw = await fh.readFile();
      } catch {
        await fh.close().catch(() => {});
        continue;
      }
      await fh.close();
      // Content-based binary sniff: NUL byte in the first 8KB. Catches
      // binaries with .json or .txt extensions (yes, this happens).
      const firstNul = raw.indexOf(0);
      if (firstNul !== -1 && firstNul < 8 * 1024) continue;
      const text = raw.toString("utf8");
      const rel = displayRel(ctx.rootDir, full);
      const lines = text.split(/\r?\n/);
      for (let li = 0; li < lines.length; li++) {
        const line = lines[li]!;
        const lineForCheck = caseSensitive ? line : line.toLowerCase();
        const hit = re ? re.test(line) : lineForCheck.includes(needle);
        if (!hit) continue;
        const display = line.length > 200 ? `${line.slice(0, 200)}…` : line;
        const out = `${rel}:${li + 1}: ${display}`;
        if (totalBytes + out.length + 1 > ctx.maxListBytes) {
          matches.push(`[… truncated at ${ctx.maxListBytes} bytes — refine pattern or path …]`);
          truncated = true;
          return;
        }
        matches.push(out);
        totalBytes += out.length + 1;
      }
      scanned++;
    }
  };
  await walk(startAbs);
  if (matches.length === 0) {
    return scanned === 0
      ? "(no files scanned — path empty or all files filtered out)"
      : `(no matches across ${scanned} file${scanned === 1 ? "" : "s"})`;
  }
  return matches.join("\n");
}
