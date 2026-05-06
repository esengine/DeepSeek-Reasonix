/** `#` writes project memory, `#g` global; `##+` stays a markdown heading; `\#` escapes and submits the literal `#`. */

import { closeSync, fstatSync, mkdirSync, openSync, readSync, writeSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { PROJECT_MEMORY_FILE } from "../../memory/project.js";

const PROJECT_HEADER = `# Reasonix project memory

Notes the user pinned via the \`#\` prompt prefix. The whole file is
loaded into the immutable system prefix every session — keep it terse.

`;

const GLOBAL_HEADER = `# Reasonix global memory

Cross-project notes the user pinned via the \`#g\` prompt prefix. Loaded
into every Reasonix session's prefix regardless of working directory.
Private to this machine — not committed anywhere.

`;

export type HashMemoryParse =
  | { kind: "memory"; note: string }
  | { kind: "memory-global"; note: string }
  | { kind: "escape"; text: string };

/** Order: escape > `##` heading > `#g <body>` (mandatory space) > `#<body>` project. */
export function detectHashMemory(text: string): HashMemoryParse | null {
  if (text.startsWith("\\#")) {
    return { kind: "escape", text: text.slice(1) };
  }
  if (!text.startsWith("#")) return null;
  // Markdown headings of level 2+ pass through to the model unchanged.
  // Only a single leading `#` (level-1 heading shape) is ambiguous; we
  // resolve that ambiguity in favor of memory write and document the
  // `\#` escape for users who want a literal H1 in the prompt.
  if (text.startsWith("##")) return null;
  // `#g <note>` — global memory. The space after `g` is mandatory so
  // notes like `#golang preference` route to project memory, not global.
  // `#g` alone (or `#g` + only whitespace) is treated as null — the
  // user clearly wanted the global form but typed no body, so we don't
  // silently fall back to project memory with body=`g`.
  if (/^#g\s*$/.test(text)) return null;
  const globalMatch = /^#g\s+(.+)$/s.exec(text);
  if (globalMatch) {
    const body = globalMatch[1]!.trim();
    if (!body) return null;
    return { kind: "memory-global", note: body };
  }
  const body = text.slice(1).trim();
  if (!body) return null;
  return { kind: "memory", note: body };
}

export interface AppendMemoryResult {
  /** Absolute path written to. */
  path: string;
  /** True iff the file did not exist before this call. */
  created: boolean;
}

export function appendProjectMemory(rootDir: string, note: string): AppendMemoryResult {
  return appendBulletToFile(join(rootDir, PROJECT_MEMORY_FILE), note, PROJECT_HEADER);
}

export const GLOBAL_MEMORY_DIR = ".reasonix";
export const GLOBAL_MEMORY_FILE = "REASONIX.md";

export function globalMemoryPath(homeDir: string = homedir()): string {
  return join(homeDir, GLOBAL_MEMORY_DIR, GLOBAL_MEMORY_FILE);
}

export function appendGlobalMemory(note: string, homeDir?: string): AppendMemoryResult {
  return appendBulletToFile(globalMemoryPath(homeDir), note, GLOBAL_HEADER);
}

function appendBulletToFile(path: string, note: string, newFileHeader: string): AppendMemoryResult {
  const trimmed = note.trim();
  if (!trimmed) throw new Error("note body cannot be empty");
  const bullet = `- ${trimmed}\n`;
  mkdirSync(dirname(path), { recursive: true });
  // One `a+` open covers both branches: O_APPEND lands every write
  // atomically at end-of-file (concurrent appenders interleave whole
  // bullets), O_CREAT creates the file when it's missing, and we use
  // `fstat().size === 0` as the "we just created it" signal to decide
  // whether to emit the file header. Single fd from open through
  // write — no path-based check between (CodeQL js/file-system-race).
  const fd = openSync(path, "a+");
  try {
    const stat = fstatSync(fd);
    if (stat.size === 0) {
      writeSync(fd, `${newFileHeader}${bullet}`);
      return { path, created: true };
    }
    // Existing file — peek the trailing byte to decide whether to
    // insert a leading newline. Same fd → no separate stat→read race.
    const tail = Buffer.alloc(1);
    readSync(fd, tail, 0, 1, stat.size - 1);
    const prefix = tail[0] !== 0x0a ? "\n" : "";
    writeSync(fd, `${prefix}${bullet}`);
    return { path, created: false };
  } finally {
    closeSync(fd);
  }
}
