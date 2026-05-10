/** REASONIX.md pinned into ImmutablePrefix.system; edits invalidate the prefix-cache fingerprint. */

import { existsSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

export const PROJECT_MEMORY_FILE = "REASONIX.md";
export const PROJECT_MEMORY_MAX_CHARS = 8000;

/** Marker filenames that signal a foreign agent-platform workspace. */
const FOREIGN_PLATFORM_FILE_MARKERS = ["SOUL.md", "AGENT.md", "PERSONA.md"] as const;

/** Returns the marker(s) that flagged rootDir as a foreign agent-platform data dir; null on a normal coding project. */
export function detectForeignAgentPlatform(rootDir: string): string[] | null {
  const hits: string[] = [];
  for (const name of FOREIGN_PLATFORM_FILE_MARKERS) {
    if (existsSync(join(rootDir, name))) hits.push(name);
  }
  if (isDir(join(rootDir, "skills")) && isDir(join(rootDir, "memories"))) {
    hits.push("skills/ + memories/");
  }
  return hits.length > 0 ? hits : null;
}

function isDir(path: string): boolean {
  try {
    return statSync(path).isDirectory();
  } catch {
    return false;
  }
}

export interface ProjectMemory {
  /** Absolute path the memory was read from. */
  path: string;
  /** Post-truncation content (may include a "… (truncated N chars)" marker). */
  content: string;
  /** Original byte length before truncation. */
  originalChars: number;
  /** True iff `originalChars > PROJECT_MEMORY_MAX_CHARS`. */
  truncated: boolean;
}

/** Empty / whitespace-only files return null so they don't perturb the cache prefix. */
export function readProjectMemory(rootDir: string): ProjectMemory | null {
  const path = join(rootDir, PROJECT_MEMORY_FILE);
  if (!existsSync(path)) return null;
  let raw: string;
  try {
    raw = readFileSync(path, "utf8");
  } catch {
    return null;
  }
  const trimmed = raw.trim();
  if (!trimmed) return null;
  const originalChars = trimmed.length;
  const truncated = originalChars > PROJECT_MEMORY_MAX_CHARS;
  const content = truncated
    ? `${trimmed.slice(0, PROJECT_MEMORY_MAX_CHARS)}\n… (truncated ${
        originalChars - PROJECT_MEMORY_MAX_CHARS
      } chars)`
    : trimmed;
  return { path, content, originalChars, truncated };
}

export function memoryEnabled(): boolean {
  const env = process.env.REASONIX_MEMORY;
  if (env === "off" || env === "false" || env === "0") return false;
  return true;
}

/** Deterministic — same memory file always yields the same prefix hash. */
export function applyProjectMemory(basePrompt: string, rootDir: string): string {
  if (!memoryEnabled()) return basePrompt;
  const mem = readProjectMemory(rootDir);
  if (!mem) return basePrompt;
  return `${basePrompt}

# Project memory (REASONIX.md)

The user pinned these notes about this project — treat them as authoritative context for every turn:

\`\`\`
${mem.content}
\`\`\`
`;
}
