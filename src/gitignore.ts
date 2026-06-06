/** Nested .gitignore evaluation — shared by the at-mention picker walker and the semantic chunker. */

import { readFileSync } from "node:fs";
import { readFile } from "node:fs/promises";
import path from "node:path";
import ignore, { type Ignore } from "ignore";
import { TtlLruCache } from "./core/lru.js";

export interface GitignoreLayer {
  /** Absolute dir the .gitignore lives in. Patterns evaluate relative to this. */
  dirAbs: string;
  ig: Ignore;
}

/** Per-keystroke at-mention pickers walk every ancestor .gitignore — cached lookups make the same-tick re-walk free. */
const gitignoreCache = new TtlLruCache<string, Ignore | null>(256, 5_000);

function buildIgnore(text: string): Ignore {
  return ignore().add(text);
}

export async function loadGitignoreAt(dirAbs: string): Promise<Ignore | null> {
  const cached = gitignoreCache.get(dirAbs);
  if (cached !== undefined) return cached;
  let result: Ignore | null;
  try {
    result = buildIgnore(await readFile(path.join(dirAbs, ".gitignore"), "utf8"));
  } catch {
    result = null;
  }
  gitignoreCache.set(dirAbs, result);
  return result;
}

export function loadGitignoreAtSync(dirAbs: string): Ignore | null {
  const cached = gitignoreCache.get(dirAbs);
  if (cached !== undefined) return cached;
  let result: Ignore | null;
  try {
    result = buildIgnore(readFileSync(path.join(dirAbs, ".gitignore"), "utf8"));
  } catch {
    result = null;
  }
  gitignoreCache.set(dirAbs, result);
  return result;
}

/** True if any layer — outermost to innermost — ignores this path. */
export function ignoredByLayers(
  layers: readonly GitignoreLayer[],
  abs: string,
  isDir: boolean,
): boolean {
  for (const layer of layers) {
    const rel = path.relative(layer.dirAbs, abs).split(path.sep).join("/");
    if (!rel || rel.startsWith("..")) continue;
    if (layer.ig.ignores(isDir ? `${rel}/` : rel)) return true;
  }
  return false;
}
