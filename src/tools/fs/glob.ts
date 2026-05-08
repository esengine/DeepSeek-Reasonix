import { promises as fs } from "node:fs";
import * as pathMod from "node:path";
import picomatch from "picomatch";

export interface GlobContext {
  rootDir: string;
  skipDirNames: ReadonlySet<string>;
}

function displayRel(rootDir: string, full: string): string {
  return pathMod.relative(rootDir, full).replaceAll("\\", "/");
}

export async function globFiles(
  ctx: GlobContext,
  startAbs: string,
  args: {
    pattern: string;
    sort_by?: "mtime" | "name";
    include_deps?: boolean;
    limit?: number;
    signal?: AbortSignal;
  },
): Promise<string> {
  if (args.signal?.aborted) {
    throw new DOMException("glob aborted by user", "AbortError");
  }
  const includeDeps = args.include_deps === true;
  const sortBy = args.sort_by ?? "mtime";
  const limit = Math.max(1, Math.min(1000, Math.floor(args.limit ?? 200)));
  const isMatch = picomatch(args.pattern, { dot: true, nocase: true });

  const hits: { rel: string; mtimeMs: number }[] = [];

  const walk = async (dir: string): Promise<void> => {
    if (args.signal?.aborted) {
      throw new DOMException("glob aborted by user", "AbortError");
    }
    let entries: import("node:fs").Dirent[];
    try {
      entries = await fs.readdir(dir, { withFileTypes: true });
    } catch {
      return;
    }
    for (const e of entries) {
      const full = pathMod.join(dir, e.name);
      if (e.isDirectory()) {
        if (!includeDeps && ctx.skipDirNames.has(e.name)) continue;
        await walk(full);
        continue;
      }
      if (!e.isFile() && !e.isSymbolicLink()) continue;
      const rel = displayRel(ctx.rootDir, full);
      if (!isMatch(rel)) continue;
      let mtimeMs = 0;
      if (sortBy === "mtime") {
        try {
          const st = await fs.stat(full);
          mtimeMs = st.mtimeMs;
        } catch {
          continue;
        }
      }
      hits.push({ rel, mtimeMs });
    }
  };
  await walk(startAbs);

  if (hits.length === 0) return "(no matches)";
  if (sortBy === "mtime") hits.sort((a, b) => b.mtimeMs - a.mtimeMs);
  else hits.sort((a, b) => a.rel.localeCompare(b.rel));

  const truncated = hits.length > limit;
  const shown = hits.slice(0, limit);
  const lines = shown.map((h) => h.rel);
  if (truncated) {
    lines.push(
      `[… ${hits.length - limit} more matches — refine pattern or raise limit (max 1000) …]`,
    );
  }
  return lines.join("\n");
}
