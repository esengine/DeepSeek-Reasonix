/** JSONL append-only message log under `~/.reasonix/sessions/`; concurrent-write safe. */

import { execFileSync } from "node:child_process";
import {
  appendFileSync,
  chmodSync,
  copyFileSync,
  existsSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  renameSync,
  statSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { homedir } from "node:os";
import { dirname, join, posix as posixPath, win32 as win32Path } from "node:path";
import type { ChatMessage } from "../types.js";

const SESSION_SIDECAR_EXTS = [
  ".events.jsonl",
  ".meta.json",
  ".pending.json",
  ".plan.json",
  ".jsonl.bak",
] as const;

/** Best-effort git branch sniff; returns undefined if not a git repo or git missing. */
export function detectGitBranch(cwd: string): string | undefined {
  try {
    const out = execFileSync("git", ["branch", "--show-current"], {
      cwd,
      stdio: ["ignore", "pipe", "ignore"],
      timeout: 800,
      encoding: "utf8",
    }).trim();
    return out || undefined;
  } catch {
    return undefined;
  }
}

export interface SessionInfo {
  name: string;
  path: string;
  size: number;
  messageCount: number;
  mtime: Date;
  meta: SessionMeta;
}

export interface SessionMeta {
  branch?: string;
  summary?: string;
  totalCostUsd?: number;
  turnCount?: number;
  /** Absolute path of the workspace root the session was created/used in. */
  workspace?: string;
  /** Wallet currency at last save — used to format `totalCostUsd` in the picker without re-fetching balance. */
  balanceCurrency?: string;
  /** Cumulative cache hit / miss tokens across the session — survives resume so /status cache% isn't 0 on a fresh boot. */
  cacheHitTokens?: number;
  cacheMissTokens?: number;
  /** Last turn's promptTokens — lets /status render the context bar before the next turn fires. */
  lastPromptTokens?: number;
  /** True when the session filename/summary was generated from conversation content. */
  autoTitleGenerated?: boolean;
}

export function sessionsDir(): string {
  return join(homedir(), ".reasonix", "sessions");
}

export function sessionPath(name: string): string {
  return join(sessionsDir(), `${sanitizeName(name)}.jsonl`);
}

export function sanitizeName(name: string): string {
  const cleaned = name.replace(/[^\w\-\u4e00-\u9fa5]/g, "_").slice(0, 64);
  return cleaned || "default";
}

/** Sortable timestamp `YYYYMMDDHHmm` — used as a session-name suffix. */
export function timestampSuffix(): string {
  return new Date().toISOString().replace(/[^\d]/g, "").slice(0, 12);
}

/** Unique name for an in-app "new session" — strips a trailing 12/14-digit timestamp from the current name and re-stamps with seconds precision so back-to-back clicks don't collide. */
export function freshSessionName(currentName: string | undefined): string {
  const base = currentName ? currentName.replace(/-\d{12,14}$/, "") : "default";
  const stamp = new Date().toISOString().replace(/[^\d]/g, "").slice(0, 14);
  return `${base || "default"}-${stamp}`;
}

/** Names of `.jsonl` sessions starting with `prefix`, newest-first by filename. */
export function findSessionsByPrefix(prefix: string): string[] {
  const dir = sessionsDir();
  if (!existsSync(dir)) return [];
  try {
    const files = readdirSync(dir)
      .filter((f) => f.endsWith(".jsonl") && !f.endsWith(".events.jsonl") && f.startsWith(prefix))
      .sort()
      .reverse();
    return files.map((f) => f.replace(/\.jsonl$/, ""));
  } catch {
    return [];
  }
}

export interface SessionPreview {
  messageCount: number;
  lastActive: Date;
}

/** Resolve launch-time session: forceNew → timestamped suffix; else latest `${name}-*` if any, else base. Preview returned only on the default branch when messages exist. */
export function resolveSession(
  sessionName: string | undefined,
  forceNew?: boolean,
  forceResume?: boolean,
): { resolved: string | undefined; preview: SessionPreview | undefined } {
  let resolved = sessionName;
  let preview: SessionPreview | undefined;

  if (sessionName && forceNew) {
    resolved = `${sessionName}-${timestampSuffix()}`;
  } else if (sessionName && !forceResume) {
    let sessionToCheck = sessionName;
    const prefixed = findSessionsByPrefix(`${sessionName}-`);
    if (prefixed.length > 0) {
      sessionToCheck = prefixed[0]!;
    }
    const prior = loadSessionMessages(sessionToCheck);
    if (prior.length > 0) {
      resolved = sessionToCheck;
      const p = sessionPath(sessionToCheck);
      const mtime = existsSync(p) ? statSync(p).mtime : new Date();
      preview = { messageCount: prior.length, lastActive: mtime };
    }
  } else if (sessionName && forceResume) {
    const prefixed = findSessionsByPrefix(`${sessionName}-`);
    if (prefixed.length > 0) {
      resolved = prefixed[0]!;
    }
  }

  return { resolved, preview };
}

export function loadSessionMessages(name: string): ChatMessage[] {
  const path = sessionPath(name);
  if (!existsSync(path)) return [];
  const live = readSessionMessages(path);
  if (live && (live.messages.length > 0 || !live.hadContent)) return live.messages;

  const backup = readSessionMessages(sessionBackupPath(path));
  return backup?.messages ?? live?.messages ?? [];
}

function readSessionMessages(
  path: string,
): { messages: ChatMessage[]; hadContent: boolean } | null {
  let raw: string;
  try {
    raw = readFileSync(path, "utf8");
  } catch {
    return null;
  }
  const out: ChatMessage[] = [];
  for (const line of raw.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    try {
      const msg = JSON.parse(trimmed) as ChatMessage;
      if (msg && typeof msg === "object" && "role" in msg) out.push(msg);
    } catch {
      /* skip malformed line */
    }
  }
  return { messages: out, hadContent: raw.trim().length > 0 };
}

export function appendSessionMessage(name: string, message: ChatMessage): void {
  const path = sessionPath(name);
  mkdirSync(dirname(path), { recursive: true });
  appendFileSync(path, `${JSON.stringify(message)}\n`, "utf8");
  try {
    chmodSync(path, 0o600);
  } catch {
    /* chmod not supported on this platform */
  }
}

export function listSessions(): SessionInfo[] {
  const dir = sessionsDir();
  if (!existsSync(dir)) return [];
  try {
    // Exclude `.events.jsonl` sidecars — they share the .jsonl suffix.
    const files = readdirSync(dir).filter(
      (f) => f.endsWith(".jsonl") && !f.endsWith(".events.jsonl"),
    );
    return files
      .map((file) => {
        const path = join(dir, file);
        const stat = statSync(path);
        const name = file.replace(/\.jsonl$/, "");
        const messageCount = countLines(path);
        return {
          name,
          path,
          size: stat.size,
          messageCount,
          mtime: stat.mtime,
          meta: loadSessionMeta(name),
        };
      })
      .sort((a, b) => b.mtime.getTime() - a.mtime.getTime());
  } catch {
    return [];
  }
}

/** Canonical form for workspace path comparisons — Windows drive-case + separator drift between session writes (yesterday) and reads (today) used to hide sessions from the sidebar. Issue #878. */
export function normalizeWorkspace(
  p: string | undefined,
  platform: NodeJS.Platform = process.platform,
): string {
  if (typeof p !== "string" || p.length === 0) return "";
  if (platform === "win32") {
    const resolved = win32Path.resolve(p);
    return resolved
      .replace(/\\/g, "/")
      .replace(/^([A-Z]):/i, (_, d: string) => `${d.toLowerCase()}:`);
  }
  return posixPath.resolve(p);
}

/** Sessions without `meta.workspace` are still hidden — resume by name still works. */
export function listSessionsForWorkspace(workspace: string): SessionInfo[] {
  const want = normalizeWorkspace(workspace);
  return listSessions().filter(
    (s) => typeof s.meta.workspace === "string" && normalizeWorkspace(s.meta.workspace) === want,
  );
}

function metaPath(name: string): string {
  return join(sessionsDir(), `${sanitizeName(name)}.meta.json`);
}

export function loadSessionMeta(name: string): SessionMeta {
  const p = metaPath(name);
  if (!existsSync(p)) return {};
  try {
    const raw = JSON.parse(readFileSync(p, "utf8")) as SessionMeta;
    return raw && typeof raw === "object" ? raw : {};
  } catch {
    return {};
  }
}

export function patchSessionMeta(name: string, patch: Partial<SessionMeta>): SessionMeta {
  const cur = loadSessionMeta(name);
  const next: SessionMeta = { ...cur, ...patch };
  const p = metaPath(name);
  mkdirSync(dirname(p), { recursive: true });
  writeFileSync(p, JSON.stringify(next), "utf8");
  try {
    chmodSync(p, 0o600);
  } catch {
    /* chmod not supported */
  }
  return next;
}

/** Renames the JSONL plus all known sidecars together; returns false if target already exists. */
export function renameSession(oldName: string, newName: string): boolean {
  const safeOld = sanitizeName(oldName);
  const safeNew = sanitizeName(newName);
  if (safeOld === safeNew) return false;
  const oldJsonl = sessionPath(oldName);
  const newJsonl = sessionPath(newName);
  if (!existsSync(oldJsonl) || existsSync(newJsonl)) return false;
  renameSync(oldJsonl, newJsonl);
  for (const ext of SESSION_SIDECAR_EXTS) {
    const oldP = oldJsonl.replace(/\.jsonl$/, ext);
    const newP = newJsonl.replace(/\.jsonl$/, ext);
    if (existsSync(oldP)) {
      try {
        renameSync(oldP, newP);
      } catch {
        /* sidecar rename failed — leave the jsonl rename in place */
      }
    }
  }
  return true;
}

/** Best-effort: per-file delete errors are swallowed so partial pruning still finishes. */
export function pruneStaleSessions(daysOld = 90): string[] {
  const cutoff = Date.now() - daysOld * 24 * 60 * 60 * 1000;
  const deleted: string[] = [];
  for (const s of listSessions()) {
    if (s.mtime.getTime() < cutoff) {
      if (deleteSession(s.name)) deleted.push(s.name);
    }
  }
  return deleted;
}

export function deleteSession(name: string): boolean {
  const path = sessionPath(name);
  try {
    unlinkSync(path);
    for (const ext of SESSION_SIDECAR_EXTS) {
      const sidecar = path.replace(/\.jsonl$/, ext);
      try {
        unlinkSync(sidecar);
      } catch {
        /* expected when the sidecar doesn't exist */
      }
    }
    return true;
  } catch {
    return false;
  }
}

/** Crash-safe rewrite: snapshot the previous live log, write a sibling tmp file, then atomically swap it in. */
export function rewriteSession(name: string, messages: ChatMessage[]): void {
  const path = sessionPath(name);
  mkdirSync(dirname(path), { recursive: true });
  const body = messages.map((m) => JSON.stringify(m)).join("\n");
  const tmp = `${path}.${process.pid}.${Date.now()}.${Math.random().toString(36).slice(2)}.tmp`;
  try {
    writeFileSync(tmp, body ? `${body}\n` : "", "utf8");
    chmodPrivate(tmp);
    if (existsSync(path) && statSync(path).size > 0) {
      const backup = sessionBackupPath(path);
      copyFileSync(path, backup);
      chmodPrivate(backup);
    }
    renameSync(tmp, path);
    chmodPrivate(path);
  } catch (err) {
    try {
      unlinkSync(tmp);
    } catch {
      /* tmp may not exist */
    }
    throw err;
  }
}

/** Rotate the live jsonl + sidecars to `<name>__archive_<ts>` so /new doesn't destroy history. Returns the archive name, or null if there was nothing to archive. */
export function archiveSession(name: string): string | null {
  const path = sessionPath(name);
  if (!existsSync(path)) return null;
  try {
    if (statSync(path).size === 0) return null;
  } catch {
    return null;
  }
  for (let attempt = 0; attempt < 5; attempt++) {
    const target = `${name}__archive_${timestampSuffix()}${attempt > 0 ? `_${attempt}` : ""}`;
    if (renameSession(name, target)) return target;
  }
  return null;
}

function countLines(path: string): number {
  try {
    const raw = readFileSync(path, "utf8");
    return raw.split(/\r?\n/).filter((l) => l.trim()).length;
  } catch {
    return 0;
  }
}

function sessionBackupPath(path: string): string {
  return `${path}.bak`;
}

function chmodPrivate(path: string): void {
  try {
    chmodSync(path, 0o600);
  } catch {
    /* chmod not supported */
  }
}
