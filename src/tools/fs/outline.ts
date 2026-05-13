/** Per-language top-level symbol outline for read_file preview. Regex-anchored at column 0 — nested decls intentionally skipped. */

import * as pathMod from "node:path";

export type OutlineEntry = { line: number; text: string };

const OUTLINE_MAX_ENTRIES = 30;
const OUTLINE_TAIL_KEEP = 5;

const TS_EXPORT_RE =
  /^export\s+(?:default\s+)?(?:async\s+)?(function|class|const|let|var|interface|type|enum)\s+\*?\s*(\w+)/;

const PY_DECL_RE = /^(?:async\s+)?(def|class)\s+(\w+)/;

const GO_DECL_RE = /^(func|type|var|const)\s+(?:\([^)]+\)\s+)?(\w+)/;

const RUST_DECL_RE =
  /^(?:pub(?:\([^)]+\))?\s+)?(?:async\s+)?(?:unsafe\s+)?(fn|struct|enum|trait|mod|type|const|static|union)\s+(\w+)/;

const RUST_IMPL_RE = /^(?:unsafe\s+)?impl(?:\s*<[^>]+>)?\s+(?:[^{]+\s+for\s+)?(\w+)/;

const MD_HEADING_RE = /^(#{1,6})\s+(.+?)\s*$/;

const MD_FENCE_RE = /^```/;

type Lang = "ts" | "py" | "go" | "rust" | "md";

const EXT_TO_LANG: Record<string, Lang> = {
  ".ts": "ts",
  ".tsx": "ts",
  ".mts": "ts",
  ".cts": "ts",
  ".js": "ts",
  ".jsx": "ts",
  ".mjs": "ts",
  ".cjs": "ts",
  ".py": "py",
  ".pyi": "py",
  ".go": "go",
  ".rs": "rust",
  ".md": "md",
  ".markdown": "md",
  ".mdx": "md",
};

export function extractOutline(filename: string, lines: readonly string[]): OutlineEntry[] {
  const ext = pathMod.extname(filename).toLowerCase();
  const lang = EXT_TO_LANG[ext];
  if (!lang) return [];
  switch (lang) {
    case "ts":
      return extractTs(lines);
    case "py":
      return extractPython(lines);
    case "go":
      return extractGo(lines);
    case "rust":
      return extractRust(lines);
    case "md":
      return extractMarkdown(lines);
  }
}

function extractTs(lines: readonly string[]): OutlineEntry[] {
  const out: OutlineEntry[] = [];
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]!;
    if (!line.startsWith("export ")) continue;
    const m = TS_EXPORT_RE.exec(line);
    if (!m) continue;
    out.push({ line: i + 1, text: `export ${m[1]} ${m[2]}` });
  }
  return out;
}

function extractPython(lines: readonly string[]): OutlineEntry[] {
  const out: OutlineEntry[] = [];
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]!;
    if (line.startsWith(" ") || line.startsWith("\t")) continue;
    const m = PY_DECL_RE.exec(line);
    if (!m) continue;
    out.push({ line: i + 1, text: `${m[1]} ${m[2]}` });
  }
  return out;
}

function extractGo(lines: readonly string[]): OutlineEntry[] {
  const out: OutlineEntry[] = [];
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]!;
    if (line.startsWith(" ") || line.startsWith("\t")) continue;
    const m = GO_DECL_RE.exec(line);
    if (!m) continue;
    out.push({ line: i + 1, text: `${m[1]} ${m[2]}` });
  }
  return out;
}

function extractRust(lines: readonly string[]): OutlineEntry[] {
  const out: OutlineEntry[] = [];
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]!;
    if (line.startsWith(" ") || line.startsWith("\t")) continue;
    const implMatch = RUST_IMPL_RE.exec(line);
    if (implMatch) {
      out.push({ line: i + 1, text: `impl ${implMatch[1]}` });
      continue;
    }
    const m = RUST_DECL_RE.exec(line);
    if (!m) continue;
    out.push({ line: i + 1, text: `${m[1]} ${m[2]}` });
  }
  return out;
}

function extractMarkdown(lines: readonly string[]): OutlineEntry[] {
  const out: OutlineEntry[] = [];
  let inFence = false;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]!;
    if (MD_FENCE_RE.test(line)) {
      inFence = !inFence;
      continue;
    }
    if (inFence) continue;
    const m = MD_HEADING_RE.exec(line);
    if (!m) continue;
    out.push({ line: i + 1, text: `${m[1]} ${m[2]}` });
  }
  return out;
}

export function formatOutline(entries: readonly OutlineEntry[]): string {
  const total = entries.length;
  if (total === 0) return "";
  const lastEntry = entries[total - 1]!;
  const width = String(lastEntry.line).length;
  const fmt = (e: OutlineEntry) => `  L${String(e.line).padStart(width, " ")}  ${e.text}`;
  const header = `[outline: ${total} symbol${total === 1 ? "" : "s"}]`;
  if (total <= OUTLINE_MAX_ENTRIES) {
    return [header, ...entries.map(fmt)].join("\n");
  }
  const headCount = OUTLINE_MAX_ENTRIES - OUTLINE_TAIL_KEEP;
  const headEntries = entries.slice(0, headCount);
  const tailEntries = entries.slice(-OUTLINE_TAIL_KEEP);
  const omitted = total - OUTLINE_MAX_ENTRIES;
  const gapStart = headEntries[headEntries.length - 1]!.line;
  const gapEnd = tailEntries[0]!.line;
  return [
    header,
    ...headEntries.map(fmt),
    `  [… ${omitted} more symbol${omitted === 1 ? "" : "s"} between L${gapStart} and L${gapEnd} …]`,
    ...tailEntries.map(fmt),
  ].join("\n");
}
