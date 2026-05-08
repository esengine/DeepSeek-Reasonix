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

function throwIfAborted(signal?: AbortSignal): void {
  if (!signal?.aborted) return;
  throw new DOMException("search aborted by user", "AbortError");
}

function displayRel(rootDir: string, full: string): string {
  return pathMod.relative(rootDir, full).replaceAll("\\", "/");
}

export async function searchFiles(
  ctx: Pick<SearchContext, "rootDir" | "maxListBytes" | "skipDirNames">,
  startAbs: string,
  args: { pattern: string; include_deps?: boolean; signal?: AbortSignal },
): Promise<string> {
  throwIfAborted(args.signal);
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
    throwIfAborted(args.signal);
    let entries: import("node:fs").Dirent[];
    try {
      entries = await fs.readdir(dir, { withFileTypes: true });
    } catch {
      return;
    }
    for (const e of entries) {
      throwIfAborted(args.signal);
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
    context?: number;
    signal?: AbortSignal;
  },
): Promise<string> {
  throwIfAborted(args.signal);
  const caseSensitive = args.case_sensitive === true;
  const includeDeps = args.include_deps === true;
  const ctxLines = Math.max(0, Math.min(20, Math.floor(args.context ?? 0)));
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

  const pushLine = (out: string): boolean => {
    if (totalBytes + out.length + 1 > ctx.maxListBytes) {
      matches.push(`[… truncated at ${ctx.maxListBytes} bytes — refine pattern or path …]`);
      truncated = true;
      return false;
    }
    matches.push(out);
    totalBytes += out.length + 1;
    return true;
  };

  const walk = async (dir: string): Promise<void> => {
    if (truncated) return;
    throwIfAborted(args.signal);
    let entries: import("node:fs").Dirent[];
    try {
      entries = await fs.readdir(dir, { withFileTypes: true });
    } catch {
      return;
    }
    for (const e of entries) {
      if (truncated) return;
      throwIfAborted(args.signal);
      if (e.isDirectory()) {
        if (!includeDeps && ctx.skipDirNames.has(e.name)) continue;
        await walk(pathMod.join(dir, e.name));
        continue;
      }
      if (!e.isFile()) continue;
      const full = pathMod.join(dir, e.name);
      if (ctx.nameMatch && !ctx.nameMatch(e.name, displayRel(ctx.rootDir, full))) continue;
      if (ctx.isBinaryByName(e.name)) continue;
      let fh: import("node:fs/promises").FileHandle;
      try {
        fh = await fs.open(full, "r");
      } catch {
        continue;
      }
      let raw: Buffer;
      try {
        throwIfAborted(args.signal);
        const st = await fh.stat();
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
      throwIfAborted(args.signal);
      const firstNul = raw.indexOf(0);
      if (firstNul !== -1 && firstNul < 8 * 1024) continue;
      const text = raw.toString("utf8");
      const rel = displayRel(ctx.rootDir, full);
      const lines = text.split(/\r?\n/);
      const hits: number[] = [];
      for (let li = 0; li < lines.length; li++) {
        throwIfAborted(args.signal);
        const line = lines[li]!;
        const lineForCheck = caseSensitive ? line : line.toLowerCase();
        const hit = re ? re.test(line) : lineForCheck.includes(needle);
        if (hit) hits.push(li);
      }
      scanned++;
      if (hits.length === 0) continue;
      if (ctxLines === 0) {
        for (const li of hits) {
          if (truncated) return;
          const line = lines[li]!;
          const display = line.length > 200 ? `${line.slice(0, 200)}…` : line;
          if (!pushLine(`${rel}:${li + 1}: ${display}`)) return;
        }
        continue;
      }
      const hitSet = new Set(hits);
      let prevWindowEnd = -2;
      for (const li of hits) {
        if (truncated) return;
        const winStart = Math.max(0, li - ctxLines);
        const winEnd = Math.min(lines.length - 1, li + ctxLines);
        if (winStart > prevWindowEnd + 1 && prevWindowEnd >= 0) {
          if (!pushLine("--")) return;
        }
        const realStart = winStart > prevWindowEnd + 1 ? winStart : prevWindowEnd + 1;
        for (let i = realStart; i <= winEnd; i++) {
          const line = lines[i]!;
          const display = line.length > 200 ? `${line.slice(0, 200)}…` : line;
          const sep = hitSet.has(i) ? ":" : "-";
          if (!pushLine(`${rel}:${i + 1}${sep} ${display}`)) return;
        }
        prevWindowEnd = winEnd;
      }
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
