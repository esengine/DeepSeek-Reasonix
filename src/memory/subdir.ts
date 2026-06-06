/** Module-scoped memory files (#1033). Walks from a file's dir up to rootDir, collecting REASONIX.md (or AGENTS.md / AGENT.md) found along the way. The root's memory is excluded — it's already in the system prompt via applyProjectMemory. */

import { existsSync, readFileSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { PROJECT_MEMORY_FILES, PROJECT_MEMORY_MAX_CHARS } from "./project.js";

/** PROJECT_MEMORY_FILES matches inside `absDir` AND its ancestors, walking up to (but not including) `rootDir`. Innermost-first. Returns absolute paths. */
export function findDirMemory(absDir: string, rootDir: string): string[] {
  const root = resolve(rootDir);
  const target = resolve(absDir);
  const rel = relative(root, target);
  if (rel.startsWith("..")) return [];
  const found: string[] = [];
  let cur = target;
  while (cur !== root) {
    const r = relative(root, cur);
    if (!r || r.startsWith("..")) break;
    for (const name of PROJECT_MEMORY_FILES) {
      const path = join(cur, name);
      if (existsSync(path)) {
        found.push(path);
        break;
      }
    }
    const parent = dirname(cur);
    if (parent === cur) break;
    cur = parent;
  }
  return found;
}

/** Ancestor PROJECT_MEMORY_FILES matches for a file at `absPath`, walking from its dir up to (but not including) `rootDir`. Innermost-first. */
export function findSubdirMemoryAncestors(absPath: string, rootDir: string): string[] {
  return findDirMemory(dirname(resolve(absPath)), rootDir);
}

export function readSubdirMemoryContent(path: string): string | null {
  let raw: string;
  try {
    raw = readFileSync(path, "utf8");
  } catch {
    return null;
  }
  const trimmed = raw.trim();
  if (!trimmed) return null;
  if (trimmed.length <= PROJECT_MEMORY_MAX_CHARS) return trimmed;
  return `${trimmed.slice(0, PROJECT_MEMORY_MAX_CHARS)}\n… (truncated ${
    trimmed.length - PROJECT_MEMORY_MAX_CHARS
  } chars)`;
}

export function formatSubdirMemorySection(displayPath: string, content: string): string {
  return `[module memory: ${displayPath}]\n\n${content}`;
}
