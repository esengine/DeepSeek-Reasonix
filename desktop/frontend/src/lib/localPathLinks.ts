// Local-path linkification for chat markdown (issue #7426).
//
// AI replies frequently print local file paths as plain text (Windows drive
// paths, UNC paths, file:/// URLs). GFM autolink literals only recognize
// http(s)/www/email, so these render as inert text. This module rewrites
// matching text nodes into markdown links whose href is a file:/// URL; the
// link click handler (RichMarkdownLink) routes those to the native
// OpenLocalPath binding instead of the system browser.

import { visit } from "unist-util-visit";
import type { Parent, Link, Root, Text } from "mdast";
import { hasDisallowedWindowsPathSyntax, isLocalFileHref } from "./localFileUrl";
import { unescapeRefPath } from "./refToken";

// Sentence punctuation is excluded from path characters: Windows forbids `：`
// in file names, and the CJK/ASCII punctuation set below almost never appears
// inside a real path. Excluding it at match time is far more reliable than
// trimming a trailing marker after the fact ("D:\x\y.md。已生成" must stop at
// `。`). `.` is deliberately kept (extensions) and only stripped when it is
// actually trailing.
const SENT_PUNCT = "，。；、！？,;!?（）";

// One path character: either a backslash-escaped space/tab (kept, matching the
// @path grammar in lib/refToken), or any non-whitespace char that is not a
// quote, angle bracket, pipe, `?`, `*`, colon or sentence punctuation.
// The `\\[ \t]` alternative must come FIRST: in a `(?:A|B)+` loop the engine
// backtracks by dropping repetitions, not by retrying the alternative at the
// same position, so a trailing `\ ` would otherwise never be consumed.
const PATH_CHAR = String.raw`(?:\\[ \t]|[^\s<>"|?*:：${SENT_PUNCT}])`;

// file URLs are matched whole (their `:` and `.` are legal there). Drive and
// UNC prefix boundaries are checked in JavaScript rather than with regular
// expression lookbehind: macOS 12's WebKit rejects `(?<!...)` while loading the
// whole lazy Markdown chunk, before any message is rendered. URL candidates are
// validated with the shared parser below so malformed file URLs stay inert.
const FILE_RE = new RegExp(String.raw`file://[^\s<>"|?*${SENT_PUNCT}]+`, "g");
const DRIVE_RE = new RegExp(String.raw`[A-Za-z]:[\\/]${PATH_CHAR}+`, "g");
// UNC share paths: the plugin runs on parsed markdown text nodes, where
// CommonMark backslash escaping has already folded `\\` into `\`. A share
// therefore arrives as `\nas\share\docs\report.md` (single leading
// backslash); linkify restores the `\\` prefix so the native opener receives
// the real UNC form. The leading `\` is mandatory (the fold always leaves
// one). The JavaScript prefix guard keeps `C:\nas` or a plain `a\b` from being
// mistaken for a share start without requiring WebKit lookbehind support.
const UNC_RE = new RegExp(
  String.raw`\\(?!\\)[^\s\\<>"|?*:：${SENT_PUNCT}]+\\${PATH_CHAR}+`,
  "g",
);

const DRIVE_PREFIX_RE = /[:/A-Za-z]/;
const UNC_PREFIX_RE = /[\\/\w:；：，。、！？（）]/;
// A file URL must start a URI-like token. Without this guard, the matcher can
// start in the middle of `profile://...` or `http://file://...` and produce a
// clickable suffix that was never a local path.
const FILE_PREFIX_CHARS = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./\\:+-?#&=";

function hasValidPrefixBoundary(text: string, start: number, kind: "file" | "drive" | "unc"): boolean {
  if (start === 0) return true;
  const previous = text[start - 1];
  if (kind === "file") return !FILE_PREFIX_CHARS.includes(previous);
  return kind === "drive" ? !DRIVE_PREFIX_RE.test(previous) : !UNC_PREFIX_RE.test(previous);
}

// Trailing closers that are more likely sentence punctuation than file name
// characters. A trailing `)` is only stripped when parens are unbalanced —
// "C:\Program Files (x86)" keeps its closing paren, "D:\x\y.md)." loses it.
const TRAIL_STRIP_RE = /[.)\]]+$/;

function stripTrailingClosers(raw: string): string {
  const stripped = raw.replace(TRAIL_STRIP_RE, "");
  if (stripped === raw) return raw;
  // A trailing `)` may be a real file-name character inside balanced parens
  // ("C:\Program Files (x86)") — only strip it when parens are unbalanced,
  // i.e. the removed part contains a `)` with no matching opener.
  if (raw.slice(stripped.length).includes(")")) {
    const open = (raw.match(/\(/g) ?? []).length;
    const close = (raw.match(/\)/g) ?? []).length;
    if (close <= open) return raw;
  }
  return stripped;
}

export interface LocalPathSegment {
  /** Raw text to render (keeps the original spelling, escapes intact). */
  text: string;
  /** When present, this segment is a clickable local path. */
  path?: string;
}

/**
 * Splits `text` into plain segments and clickable local-path segments.
 * Pure function — unit tests cover the full recognition matrix here.
 */
export function linkifyLocalPaths(text: string): LocalPathSegment[] {
  const matches: Array<{ start: number; end: number; raw: string; kind: "file" | "drive" | "unc" }> = [];
  const patterns: Array<[RegExp, "file" | "drive" | "unc"]> = [
    [FILE_RE, "file"],
    [DRIVE_RE, "drive"],
    [UNC_RE, "unc"],
  ];
  for (const [re, kind] of patterns) {
    re.lastIndex = 0;
    let m: RegExpExecArray | null;
    while ((m = re.exec(text)) !== null) {
      const match = m;
      const matchEnd = match.index + match[0].length;
      if (!hasValidPrefixBoundary(text, match.index, kind)) {
        continue;
      }
      if (kind === "file" && !isLocalFileHref(stripTrailingClosers(match[0]))) {
        continue;
      }
      // FILE_RE stops before a question mark so it cannot consume query
      // syntax. Do not linkify the safe-looking prefix of a file URL that has
      // a raw query suffix; localPathFromHref intentionally rejects queries.
      if (kind === "file" && text[matchEnd] === "?") {
        continue;
      }
      // Do not turn the safe-looking prefix of an alternate data stream into
      // a link (for example, C:\\report.md in C:\\report.md:payload).
      if (kind !== "file" && text[matchEnd] === ":" && !/\s/.test(text[matchEnd + 1] ?? "")) {
        continue;
      }
      // RegExpExecArray has no start/end — compare via index/length.
      const overlapped = matches.some((p) => !(matchEnd <= p.start || match.index >= p.end));
      if (!overlapped) {
        matches.push({ start: match.index, end: matchEnd, raw: match[0], kind });
      }
      if (match.index === re.lastIndex) re.lastIndex += 1; // guard against zero-width
    }
  }
  matches.sort((a, b) => a.start - b.start);

  const segments: LocalPathSegment[] = [];
  let cursor = 0;
  for (const m of matches) {
    if (m.start > cursor) segments.push({ text: text.slice(cursor, m.start) });
    // UNC text arrives with a single leading backslash (markdown folded the
    // `\\` escape); restore the real UNC prefix for the native opener.
    const path = m.kind === "unc" ? "\\" + stripTrailingClosers(m.raw) : stripTrailingClosers(m.raw);
    const decodedPath = unescapeRefPath(path);
    if (path && !hasDisallowedWindowsPathSyntax(decodedPath)) {
      segments.push({ text: m.raw, path: decodedPath });
    } else {
      segments.push({ text: m.raw });
    }
    cursor = Math.max(cursor, m.end);
  }
  if (cursor < text.length) segments.push({ text: text.slice(cursor) });
  return segments;
}

/**
 * Builds the href for a clickable local path. Uses file:/// with forward
 * slashes (the standard Windows form) and percent-encodes non-ASCII and `#`.
 * Valid file URLs are preserved verbatim so authority-form UNC URLs are not
 * rewritten into a different path.
 */
export function localPathHref(path: string): string {
  if (isLocalFileHref(path)) return path;
  if (path.startsWith("file:///")) {
    try {
      path = decodeURIComponent(path.slice("file:///".length));
    } catch {
      // Malformed escapes: keep the literal text rather than failing.
    }
  }
  return "file:///" + encodeURI(path.replace(/\\/g, "/")).replace(/#/g, "%23");
}

/**
 * Remark plugin: rewrites plain-text nodes containing local paths into link
 * nodes (text + link alternation), so the markdown renderer hands them to
 * RichMarkdownLink which routes them to the native opener.
 *
 * Collection and replacement are separate passes: replacing during the visit
 * would re-visit the freshly inserted link children and nest anchors.
 */
export function remarkLocalPathLinks() {
  return (tree: Root) => {
    const plan: Array<{ parent: Parent; index: number; nodes: Array<Text | Link> }> = [];
    visit(tree, "text", (node: Text, index: number | undefined, parent) => {
      if (parent === undefined || parent === null || index === undefined || index === null) return;
      // Skip link internals: rewriting their text would nest <a> elements
      // (mdast forbids links inside links) and double-fire on click.
      if (parent.type === "link" || parent.type === "linkReference") return;
      const segments = linkifyLocalPaths(node.value);
      if (segments.length === 1 && segments[0].path === undefined) return;
      plan.push({
        parent,
        index,
        nodes: segments.map<Text | Link>((seg) =>
          seg.path !== undefined
            ? {
                type: "link",
                url: localPathHref(seg.path),
                title: null,
                children: [{ type: "text", value: seg.text }],
              }
            : { type: "text", value: seg.text },
        ),
      });
    });
    // Back-to-front keeps earlier indices valid within the same parent.
    for (let i = plan.length - 1; i >= 0; i--) {
      const { parent, index, nodes } = plan[i];
      parent.children.splice(index, 1, ...nodes);
    }
  };
}
