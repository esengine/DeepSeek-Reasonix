import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import { loadCheckpoint } from "../../code/checkpoints.js";
import { lineDiff } from "../../tools/fs/edit.js";
import type { DashboardContext } from "../context.js";
import type { ApiResult } from "../router.js";

export interface FileDiff {
  file: string;
  additions: number;
  deletions: number;
  patch?: string;
  status: "added" | "deleted" | "modified";
}

export async function handleCheckpointDiffs(
  method: string,
  _rest: string[],
  _body: string,
  ctx: DashboardContext,
  query: URLSearchParams = new URLSearchParams(),
): Promise<ApiResult> {
  if (method !== "GET") return { status: 405, body: { error: "GET only" } };

  const rootDir = ctx.getCurrentCwd?.();
  if (!rootDir) return { status: 200, body: [] };

  const checkpointId = query.get("id");
  if (!checkpointId) return { status: 400, body: { error: "missing id" } };

  const checkpoint = loadCheckpoint(rootDir, checkpointId);
  if (!checkpoint) return { status: 404, body: { error: "checkpoint not found" } };

  const diffs: FileDiff[] = [];

  for (const snap of checkpoint.files) {
    const absPath = resolve(rootDir, snap.path);
    let currentContent: string | null = null;
    try {
      currentContent = await readFile(absPath, "utf8");
    } catch {
      currentContent = null;
    }

    // Snapshot says file existed
    if (snap.content !== null) {
      if (currentContent === null) {
        // File was deleted since checkpoint
        diffs.push({
          file: snap.path,
          additions: 0,
          deletions: snap.content.split("\n").length,
          status: "deleted",
        });
      } else if (currentContent !== snap.content) {
        // File was modified — use project's own lineDiff
        const rows = lineDiff(snap.content.split("\n"), currentContent.split("\n"));
        const additions = rows.filter((r) => r.op === "+").length;
        const deletions = rows.filter((r) => r.op === "-").length;
        // Build valid unified diff with @@ hunk header
        let patch = `--- a/${snap.path}\n+++ b/${snap.path}\n`;
        // Chunk into hunks with @@ headers (3 lines context)
        const ctx = 3;
        let i = 0;
        while (i < rows.length) {
          while (i < rows.length && rows[i]!.op === " ") i++;
          if (i >= rows.length) break;
          const hunkStart = Math.max(0, i - ctx);
          let hunkEnd = i;
          while (hunkEnd < rows.length && rows[hunkEnd]!.op !== " ") hunkEnd++;
          hunkEnd = Math.min(rows.length, hunkEnd + ctx);
          // Count lines in hunk for old/new
          const oldCount = rows.slice(hunkStart, hunkEnd).filter((r) => r.op !== "+").length;
          const newCount = rows.slice(hunkStart, hunkEnd).filter((r) => r.op !== "-").length;
          patch += `@@ -${hunkStart + 1},${oldCount} +${hunkStart + 1},${newCount} @@\n`;
          for (let j = hunkStart; j < hunkEnd; j++) {
            patch += `${rows[j]!.op}${rows[j]!.line}\n`;
          }
          i = hunkEnd;
        }
        diffs.push({
          file: snap.path,
          additions,
          deletions,
          patch,
          status: "modified",
        });
      }
      // else: unchanged — skip
    } else {
      // Snapshot says file didn't exist
      if (currentContent !== null) {
        // New file added since checkpoint
        const additions = currentContent.split("\n").length;
        diffs.push({
          file: snap.path,
          additions,
          deletions: 0,
          status: "added",
        });
      }
    }
  }

  // Check for files that exist on disk but weren't in the checkpoint
  // (these are new files added after the checkpoint was created)
  // For a complete picture, we'd need to walk the tree — skipping for now
  // as the checkpoint scope covers what was snapshotted.

  return { status: 200, body: diffs };
}
