import { readdir, stat } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";
import { listSessionsAsync } from "../../memory/session.js";
import { VERSION } from "../../version.js";
import type { DashboardContext } from "../context.js";
import type { ApiResult } from "../router.js";

interface DirStat {
  path: string;
  exists: boolean;
  fileCount: number;
  totalBytes: number;
}

/** Sum file sizes one level deep. Recursion deferred until we have a use-case for nested data dirs. */
async function dirSize(path: string): Promise<DirStat> {
  let fileCount = 0;
  let totalBytes = 0;
  try {
    const entries = await readdir(path);
    for (const name of entries) {
      const full = join(path, name);
      try {
        const s = await stat(full);
        if (s.isFile()) {
          fileCount++;
          totalBytes += s.size;
        } else if (s.isDirectory()) {
          try {
            const inner = await readdir(full);
            for (const child of inner) {
              try {
                const cs = await stat(join(full, child));
                if (cs.isFile()) {
                  fileCount++;
                  totalBytes += cs.size;
                }
              } catch {
                /* skip */
              }
            }
          } catch {
            /* skip */
          }
        }
      } catch {
        /* skip — file might have been deleted between readdir + stat */
      }
    }
  } catch {
    return { path, exists: false, fileCount: 0, totalBytes: 0 };
  }
  return { path, exists: true, fileCount, totalBytes };
}

export async function handleHealth(
  method: string,
  _rest: string[],
  _body: string,
  ctx: DashboardContext,
): Promise<ApiResult> {
  if (method !== "GET") {
    return { status: 405, body: { error: "GET only" } };
  }
  const home = homedir();
  const reasonixHome = join(home, ".reasonix");

  const [sessionsStat, memoryStat, semanticStat] = await Promise.all([
    dirSize(join(reasonixHome, "sessions")),
    dirSize(join(reasonixHome, "memory")),
    dirSize(join(reasonixHome, "semantic")),
  ]);

  let usageBytes = 0;
  try {
    usageBytes = (await stat(ctx.usageLogPath)).size;
  } catch {
    /* ignore */
  }

  const sessions = await listSessionsAsync();

  return {
    status: 200,
    body: {
      version: VERSION,
      latestVersion: ctx.getLatestVersion?.() ?? null,
      reasonixHome,
      sessions: {
        path: sessionsStat.path,
        count: sessions.length,
        totalBytes: sessionsStat.totalBytes,
      },
      memory: {
        path: memoryStat.path,
        fileCount: memoryStat.fileCount,
        totalBytes: memoryStat.totalBytes,
      },
      semantic: {
        path: semanticStat.path,
        exists: semanticStat.exists,
        fileCount: semanticStat.fileCount,
        totalBytes: semanticStat.totalBytes,
      },
      usageLog: {
        path: ctx.usageLogPath,
        bytes: usageBytes,
      },
      jobs: ctx.jobs ? ctx.jobs.list().length : null,
    },
  };
}
