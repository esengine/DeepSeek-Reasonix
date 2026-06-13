// Post-process completed assistant message text to link workspace file paths.
// Strategy: build a set of known file paths from the workspace, then scan the
// text for tokens that match. Only turn-completed text is processed — we
// never touch streaming content.

import { buildBasenameIndex } from "./workspaceFileSet";

// Common file extensions used as a baseline pre-filter so small or new projects
// (with few files) still benefit from linkification without needing a large
// workspace scan to build a useful extension set.
const BASE_EXTS = new Set([
  ".go", ".ts", ".tsx", ".js", ".jsx", ".md", ".json", ".yaml", ".yml",
  ".toml", ".css", ".scss", ".html", ".svg", ".sql", ".proto", ".rs",
  ".py", ".c", ".h", ".cpp", ".hpp", ".java", ".kt", ".swift", ".sh",
  ".bash", ".zsh", ".env", ".cfg", ".ini", ".xml", ".txt", ".csv",
]);

// Fenced code block delimiter pattern.
const FENCE_RE = /^(```|~~~)/;

// buildExtSet returns the union of BASE_EXTS and every unique extension
// actually present in fileSet. This dynamically adapts to the project while
// keeping a reasonable baseline so small workspaces don't miss common files.
function buildExtSet(fileSet: Set<string>): Set<string> {
  const exts = new Set(BASE_EXTS);
  for (const path of fileSet) {
    const dot = path.lastIndexOf(".");
    if (dot >= 0) exts.add(path.slice(dot).toLowerCase());
  }
  return exts;
}

// linkifyPaths scans markdown text for known workspace file paths and wraps
// them in Markdown link syntax [path](path). Returns the transformed text.
// Only runs on completed (non-streaming) text.
export function linkifyPaths(markdown: string, fileSet: Set<string>): string {
  if (!fileSet || fileSet.size === 0) return markdown;

  const basenameIndex = buildBasenameIndex(fileSet);
  const extSet = buildExtSet(fileSet);

  // Step 1: extract fenced code blocks to protect them from replacement.
  const lines = markdown.split("\n");
  const result: string[] = [];
  let inFence = false;
  let fenceChar = "";

  for (const line of lines) {
    const m = FENCE_RE.exec(line);
    if (m) {
      if (!inFence) {
        inFence = true;
        fenceChar = m[1];
      } else if (m[1] === fenceChar) {
        inFence = false;
        fenceChar = "";
      }
      result.push(line);
      continue;
    }
    if (inFence) {
      result.push(line);
      continue;
    }
    result.push(linkifyLine(line, fileSet, basenameIndex, extSet));
  }
  return result.join("\n");
}

function linkifyLine(
  line: string,
  fileSet: Set<string>,
  basenameIndex: Map<string, string | null>,
  extSet: Set<string>,
): string {
  const tokens = tokenize(line);
  const out: string[] = [];

  for (const token of tokens) {
    if (token.kind !== "word") {
      out.push(token.text);
      continue;
    }

    const word = token.text;

    // First, try the bare word as-is.
    let link = tryMatch(word, word, fileSet, basenameIndex, extSet);
    if (link === null) {
      // Try stripping wrappers in sequence: backticks, surrounding punct, style.
      const stripped = [
        word.replace(/^`+|`+$/g, ""),         // backticks
        stripSurroundingPunct(word),           // Chinese/English punct
        stripStyleWrappers(word),              // bold/italic
      ];
      for (const candidate of stripped) {
        if (candidate === "" || candidate === word) continue;
        link = tryMatch(word, candidate, fileSet, basenameIndex, extSet);
        if (link !== null) break;
      }
    }

    if (link !== null) {
      out.push(link);
    } else {
      out.push(word);
    }
  }

  return out.join("");
}

// stripStyleWrappers removes bold (** or __) and italic (* or _) Markdown
// wrappers from a token, returning the inner text or the original if no
// wrappers match.
function stripStyleWrappers(word: string): string {
  if (word.startsWith("**") && word.endsWith("**")) return word.slice(2, -2);
  if (word.startsWith("__") && word.endsWith("__")) return word.slice(2, -2);
  if (word.startsWith("*") && word.endsWith("*")) return word.slice(1, -1);
  if (word.startsWith("_") && word.endsWith("_")) return word.slice(1, -1);
  return word;
}

// tryMatch attempts to create a Markdown link [word](target) for a candidate
// path. Returns the link string on success, or null if no match.
function tryMatch(
  word: string,
  candidate: string,
  fileSet: Set<string>,
  basenameIndex: Map<string, string | null>,
  extSet: Set<string>,
): string | null {
  // Fast path: must contain "/" or end with a workspace-known extension.
  const hasSlash = candidate.includes("/");
  const ext = lastExt(candidate);
  if (!hasSlash && !extSet.has(ext)) {
    return null;
  }

  // Try exact match (full path in file set).
  if (fileSet.has(candidate)) {
    return `[${word}](${candidate})`;
  }

  // Try basename match (unique basenames only).
  const base = candidate.split("/").pop()!;
  const unambiguous = basenameIndex.get(base);
  if (unambiguous && unambiguous !== null) {
    if (candidate === base) {
      return `[${word}](${unambiguous})`;
    }
  }

  return null;
}

// stripSurroundingPunct removes non-path characters from the start and end
// of a string, leaving only [a-zA-Z0-9._/-] which are valid in file paths.
//   "文件：my-docs/…md" → "my-docs/…md"
//   "（internal/input.go）" → "internal/input.go"
function stripSurroundingPunct(s: string): string {
  const VALID_PATH_CHARS = /[a-zA-Z0-9._\-\/]/;
  let start = 0;
  while (start < s.length && !VALID_PATH_CHARS.test(s[start])) start++;
  let end = s.length;
  while (end > start && !VALID_PATH_CHARS.test(s[end - 1])) end--;
  return s.slice(start, end);
}

// Tokenize splits text into word and non-word segments while preserving
// original whitespace and punctuation.
type Token = { kind: "word" | "space"; text: string };

function tokenize(line: string): Token[] {
  const tokens: Token[] = [];
  let i = 0;
  while (i < line.length) {
    // Whitespace run
    if (/\s/.test(line[i])) {
      let j = i;
      while (j < line.length && /\s/.test(line[j])) j++;
      tokens.push({ kind: "space", text: line.slice(i, j) });
      i = j;
      continue;
    }
    // Word-like run (non-whitespace)
    let j = i;
    while (j < line.length && !/\s/.test(line[j])) j++;
    tokens.push({ kind: "word", text: line.slice(i, j) });
    i = j;
  }
  return tokens;
}

function lastExt(word: string): string {
  const dot = word.lastIndexOf(".");
  if (dot < 0) return "";
  return word.slice(dot).toLowerCase();
}
