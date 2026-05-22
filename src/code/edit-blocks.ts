/** SEARCH must match byte-for-byte; empty SEARCH = create new file. No fuzzy match — silent wrong edit beats a missing one. */

import {
  closeSync,
  existsSync,
  fstatSync,
  ftruncateSync,
  mkdirSync,
  openSync,
  readFileSync,
  readSync,
  unlinkSync,
  writeFileSync,
  writeSync,
} from "node:fs";
import { dirname, isAbsolute, relative, resolve } from "node:path";
import { type FileEncoding, decodeFileBuffer, encodeFile } from "./file-encoding.js";

export interface EditBlock {
  /** Path as written by the model — relative to rootDir, or absolute. */
  path: string;
  /** Literal text to match in the target file. Empty → create new file. */
  search: string;
  /** Replacement text to write in place of `search`. */
  replace: string;
  /** Char offset in the source message where this block started. */
  offset: number;
}

export type ApplyStatus =
  /** Edit landed on disk. */
  | "applied"
  /** New file created (SEARCH was empty and file didn't exist). */
  | "created"
  /** File exists but SEARCH block wasn't found in its content. */
  | "not-found"
  /** File doesn't exist and SEARCH was non-empty (can't create without content). */
  | "file-missing"
  /** Path escapes rootDir — refused on safety grounds. */
  | "path-escape"
  /** fs write / read threw. */
  | "error";

export interface ApplyResult {
  path: string;
  status: ApplyStatus;
  /** Extra detail (e.g. error message) for logs. */
  message?: string;
}

// `^` + `m` keeps a JS string containing `<<<<<<< SEARCH` from matching as a real block.
// `\n?` makes empty SEARCH/REPLACE bodies legal (new-file / future delete sentinels).
const BLOCK_RE = /^(\S[^\n]*)\n<{7} SEARCH\n([\s\S]*?)\n?={7}\n([\s\S]*?)\n?>{7} REPLACE/gm;

export function parseEditBlocks(text: string): EditBlock[] {
  const out: EditBlock[] = [];
  BLOCK_RE.lastIndex = 0;
  let m: RegExpExecArray | null = BLOCK_RE.exec(text);
  while (m !== null) {
    out.push({
      path: m[1]!.trim(),
      search: m[2]!,
      replace: m[3]!,
      offset: m.index,
    });
    m = BLOCK_RE.exec(text);
  }
  return out;
}

function resolveEditPath(rootDir: string, rawPath: string): string {
  const absRoot = resolve(rootDir);
  if (/^[A-Za-z]:[\\/]/.test(rawPath) || looksLikeAbsoluteSystemPath(rawPath)) {
    return resolve(rawPath);
  }
  let rooted = rawPath;
  while (rooted.startsWith("/") || rooted.startsWith("\\")) {
    rooted = rooted.slice(1);
  }
  return resolve(absRoot, rooted || ".");
}

function looksLikeAbsoluteSystemPath(rawPath: string): boolean {
  return /^\/(?:home|Users|etc|var|opt|tmp|usr|mnt|Library|Volumes|proc|sys|dev|run|srv|media|Applications|System|root|boot|private)(?:[/\\]|$)/.test(
    rawPath,
  );
}

function pathIsUnder(child: string, parent: string): boolean {
  const rel = relative(parent, child);
  return rel === "" || (!rel.startsWith("..") && !isAbsolute(rel));
}

export function applyEditBlock(block: EditBlock, rootDir: string): ApplyResult {
  const absRoot = resolve(rootDir);
  const absTarget = resolveEditPath(rootDir, block.path);
  // Refuse paths that escape rootDir. `resolve` normalizes `..`, so
  // relative-path containment avoids prefix false positives.
  if (!pathIsUnder(absTarget, absRoot)) {
    return {
      path: block.path,
      status: "path-escape",
      message: `resolved path ${absTarget} is outside rootDir ${absRoot}`,
    };
  }

  const searchEmpty = block.search.length === 0;

  // Branch on intent first so each path makes exactly one `open` call
  // — keeps CodeQL's flow analyser from tripping over a check→use
  // chain across two opens (js/file-system-race).
  if (searchEmpty) {
    try {
      mkdirSync(dirname(absTarget), { recursive: true });
      const fd = openSync(absTarget, "wx");
      try {
        writeSync(fd, block.replace);
      } finally {
        closeSync(fd);
      }
      return { path: block.path, status: "created" };
    } catch (err) {
      const e = err as NodeJS.ErrnoException;
      if (e.code === "EEXIST") {
        return {
          path: block.path,
          status: "not-found",
          message: "empty SEARCH only creates new files — this file already exists",
        };
      }
      return { path: block.path, status: "error", message: e.message };
    }
  }

  try {
    // Modify path. ENOENT is reported as `file-missing` so the model
    // knows it needs an empty SEARCH to create the file.
    let fd: number;
    try {
      fd = openSync(absTarget, "r+");
    } catch (err) {
      if ((err as NodeJS.ErrnoException).code === "ENOENT") {
        return {
          path: block.path,
          status: "file-missing",
          message: "file does not exist; to create it, use an empty SEARCH block",
        };
      }
      throw err;
    }

    try {
      const stat = fstatSync(fd);
      const inBuf = Buffer.alloc(stat.size);
      let readBytes = 0;
      while (readBytes < stat.size) {
        const n = readSync(fd, inBuf, readBytes, stat.size - readBytes, readBytes);
        if (n <= 0) break;
        readBytes += n;
      }
      const { text: content, encoding } = decodeFileBuffer(inBuf.subarray(0, readBytes));
      const le = lineEndingOf(content);
      const adaptedSearch = block.search.replace(/\r?\n/g, le);
      const adaptedReplace = block.replace.replace(/\r?\n/g, le);
      const idx = content.indexOf(adaptedSearch);
      if (idx === -1) {
        return {
          path: block.path,
          status: "not-found",
          message: "SEARCH text does not match the current file content exactly",
        };
      }
      const nextIdx = content.indexOf(adaptedSearch, idx + 1);
      if (nextIdx !== -1) {
        return {
          path: block.path,
          status: "not-found",
          message: "SEARCH text appears multiple times; include more context to disambiguate",
        };
      }
      // Apply one unambiguous occurrence. Auto-expanding to replace-all is
      // a footgun when the same string legitimately appears in several
      // unrelated places.
      const replaced = `${content.slice(0, idx)}${adaptedReplace}${content.slice(idx + adaptedSearch.length)}`;
      // Truncate first so a shorter result doesn't leave stale tail
      // bytes; ftruncate also pads with NUL when the new length is
      // longer, which we then overwrite below.
      const outBuf = encodeFile(replaced, encoding);
      ftruncateSync(fd, outBuf.length);
      let written = 0;
      while (written < outBuf.length) {
        const n = writeSync(fd, outBuf, written, outBuf.length - written, written);
        if (n <= 0) break;
        written += n;
      }
      return { path: block.path, status: "applied" };
    } finally {
      closeSync(fd);
    }
  } catch (err) {
    return { path: block.path, status: "error", message: (err as Error).message };
  }
}

export function applyEditBlocks(blocks: EditBlock[], rootDir: string): ApplyResult[] {
  return blocks.map((b) => applyEditBlock(b, rootDir));
}

export function toWholeFileEditBlock(path: string, content: string, rootDir: string): EditBlock {
  const abs = resolveEditPath(rootDir, path);
  let search = "";
  if (existsSync(abs)) {
    try {
      search = decodeFileBuffer(readFileSync(abs)).text;
    } catch {
      search = "";
    }
  }
  return { path, search, replace: content, offset: 0 };
}

export interface EditSnapshot {
  /** Path relative to rootDir, as the block named it. */
  path: string;
  /** `null` = file didn't exist; restore means delete. */
  prevContent: string | null;
  /** Encoding the file used before the edit. Required to round-trip GB18030 / UTF-8-BOM on restore. */
  prevEncoding?: FileEncoding;
}

/** De-duped by path — one "before" snapshot per file even with multiple blocks. */
export function snapshotBeforeEdits(blocks: EditBlock[], rootDir: string): EditSnapshot[] {
  const absRoot = resolve(rootDir);
  const seen = new Set<string>();
  const snapshots: EditSnapshot[] = [];
  for (const b of blocks) {
    const abs = resolveEditPath(rootDir, b.path);
    if (!pathIsUnder(abs, absRoot)) continue;
    if (seen.has(abs)) continue;
    seen.add(abs);
    if (!existsSync(abs)) {
      snapshots.push({ path: b.path, prevContent: null });
      continue;
    }
    try {
      const { text, encoding } = decodeFileBuffer(readFileSync(abs));
      snapshots.push({ path: b.path, prevContent: text, prevEncoding: encoding });
    } catch {
      // Unreadable (permission / binary) — record null so we at least
      // don't pretend the snapshot is authoritative. The restore path
      // will treat null as "delete on undo", which is wrong in that
      // case but the file wasn't ours to begin with.
      snapshots.push({ path: b.path, prevContent: null });
    }
  }
  return snapshots;
}

export function restoreSnapshots(snapshots: EditSnapshot[], rootDir: string): ApplyResult[] {
  const absRoot = resolve(rootDir);
  return snapshots.map((snap) => {
    const abs = resolveEditPath(rootDir, snap.path);
    if (!pathIsUnder(abs, absRoot)) {
      return {
        path: snap.path,
        status: "path-escape",
        message: "snapshot path escapes rootDir — refusing to restore",
      };
    }
    try {
      if (snap.prevContent === null) {
        if (existsSync(abs)) unlinkSync(abs);
        return {
          path: snap.path,
          status: "applied",
          message: "removed (the edit had created it)",
        };
      }
      writeFileSync(abs, encodeFile(snap.prevContent, snap.prevEncoding ?? "utf8"));
      return {
        path: snap.path,
        status: "applied",
        message: "restored to pre-edit content",
      };
    } catch (err) {
      return { path: snap.path, status: "error", message: (err as Error).message };
    }
  });
}

function lineEndingOf(text: string): string {
  return text.includes("\r\n") ? "\r\n" : "\n";
}
