/** JSONL append-only message log under `~/.reasonix/sessions/`; concurrent-write safe. */

import { execFileSync } from "node:child_process";
import { createHash, randomBytes } from "node:crypto";
import {
  appendFileSync,
  chmodSync,
  closeSync,
  copyFileSync,
  existsSync,
  fstatSync,
  mkdirSync,
  openSync,
  readFileSync,
  readSync,
  readdirSync,
  renameSync,
  statSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import {
  appendFile,
  chmod,
  copyFile,
  mkdir,
  open,
  readFile,
  readdir,
  rename,
  stat,
  unlink,
  writeFile,
} from "node:fs/promises";
import { homedir } from "node:os";
import { dirname, join, posix as posixPath, win32 as win32Path } from "node:path";
import { atomicWrite, atomicWriteSync } from "../core/atomic-write.js";
import type { CacheDiagnosticEntry } from "../telemetry/cache-diagnostics.js";
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
  /** How this item matched a workspace-scoped list. */
  workspaceStatus?: "matched" | "legacy_missing_meta";
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
  /** Cumulative completion (output) tokens across the session. */
  totalCompletionTokens?: number;
  /** Last turn's promptTokens — lets /status render the context bar before the next turn fires. */
  lastPromptTokens?: number;
  /** Recent per-turn cache evidence. Backward-compatible: absent on sessions created before cache diagnostics. */
  cacheDiagnostics?: CacheDiagnosticEntry[];
  /** True when the session filename/summary was generated from conversation content. */
  autoTitleGenerated?: boolean;
  /** SHA-256[:16] of the system prompt active when the session was last used.
   *  Compared on resume to warn the user if REASONIX.md or memory changed (#2212). */
  systemFingerprint?: string;
  /** Source app when the session was imported from another local AI client. */
  importedSource?: "claude" | "codex";
  /** Absolute path of the source transcript used for import. */
  importedPath?: string;
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

const READ_CHUNK_SIZE = 65536; // Balance I/O overhead vs read amplification for tail scans.

/**
 * Backward JSONL scanner — reads tail N messages. Falls back to full file read.
 */
export function readTailMessages(path: string, count: number): ChatMessage[] {
  try {
    const fd = openSync(path, "r");
    try {
      const { size } = fstatSync(fd);
      if (size === 0) return [];
      const out: ChatMessage[] = [];
      let pos = size;
      let leftover = "";
      while (pos > 0 && out.length < count) {
        const chunkSize = Math.min(READ_CHUNK_SIZE, pos);
        pos -= chunkSize;
        const buf = Buffer.alloc(chunkSize);
        readSync(fd, buf, 0, chunkSize, pos);
        const chunk = buf.toString("utf8") + leftover;
        const lines = chunk.split("\n");
        // First chunk's start may split a line; carry it over to the next iteration.
        leftover = lines[0]!;
        // Process complete lines from the tail of this chunk backward.
        for (let i = lines.length - 1; i >= 1 && out.length < count; i--) {
          const trimmed = lines[i]!.trim();
          if (!trimmed) continue;
          try {
            const msg = JSON.parse(trimmed) as ChatMessage;
            if (msg && typeof msg === "object" && "role" in msg) out.push(msg);
          } catch {
            /* skip malformed */
          }
        }
      }
      // Catch the partial line from the very first read chunk.
      if (out.length < count && leftover.trim()) {
        try {
          const msg = JSON.parse(leftover.trim()) as ChatMessage;
          if (msg && typeof msg === "object" && "role" in msg) out.push(msg);
        } catch {
          /* skip */
        }
      }
      return out.reverse();
    } finally {
      closeSync(fd);
    }
  } catch {
    return loadSessionMessagesFromPath(path);
  }
}

function loadSessionMessagesFromPath(path: string): ChatMessage[] {
  const raw = readSessionMessages(path);
  return raw?.messages ?? [];
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
  invalidatePromptHistoryCache();
}

export function listSessions(opts?: {
  workspaceFilter?: string;
  includeLegacyWorkspaceMatches?: boolean;
}): SessionInfo[] {
  const dir = sessionsDir();
  if (!existsSync(dir)) return [];
  const want = opts?.workspaceFilter ? normalizeWorkspace(opts.workspaceFilter) : null;
  const legacyPrefix =
    want && opts?.includeLegacyWorkspaceMatches
      ? legacySessionPrefixForWorkspace(opts.workspaceFilter!)
      : null;
  try {
    // Exclude `.events.jsonl` sidecars — they share the .jsonl suffix.
    const files = readdirSync(dir).filter(
      (f) => f.endsWith(".jsonl") && !f.endsWith(".events.jsonl"),
    );
    return files
      .flatMap((file) => {
        const path = join(dir, file);
        const name = file.replace(/\.jsonl$/, "");
        const meta = loadSessionMeta(name);
        // Workspace pre-filter: cheap meta read first, skip the
        // (potentially multi-MB) jsonl read for sessions that don't
        // belong to the current workspace. Issue #1179.
        let workspaceStatus: SessionInfo["workspaceStatus"] | undefined;
        if (want !== null) {
          if (typeof meta.workspace === "string") {
            if (normalizeWorkspace(meta.workspace) !== want) return [];
            workspaceStatus = "matched";
          } else if (legacyPrefix && name.startsWith(legacyPrefix)) {
            workspaceStatus = "legacy_missing_meta";
          } else {
            return [];
          }
        }
        const stat = statSync(path);
        const messageCount = countLines(path);
        return [
          { name, path, size: stat.size, messageCount, mtime: stat.mtime, meta, workspaceStatus },
        ];
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

export function listSessionsForWorkspace(workspace: string): SessionInfo[] {
  return listSessions({ workspaceFilter: workspace, includeLegacyWorkspaceMatches: true });
}

export type PromptHistoryDirection = "older" | "newer";

export interface PromptHistoryCursor {
  sessionName: string;
  /** Zero-based message index inside the persisted session JSONL. */
  messageIndex: number;
}

export interface PromptHistoryEntry {
  value: string;
  cursor: PromptHistoryCursor;
}

export interface PromptHistoryStepOptions {
  direction: PromptHistoryDirection;
  cursor?: PromptHistoryCursor | null;
  startSessionName?: string | null;
  stopSessionName?: string | null;
  workspace?: string;
}

interface PromptHistorySessionInfo {
  name: string;
  path: string;
  mtime: Date;
  workspace?: string;
}

interface PromptHistorySessionCache {
  sessions: PromptHistorySessionInfo[];
  lastUpdated: number;
}

let globalPromptHistoryCache: PromptHistorySessionCache | null = null;
// Invalidation is the correctness path; TTL only backs up external mtime changes.
const CACHE_TTL_MS = 5000;

function invalidatePromptHistoryCache(): void {
  globalPromptHistoryCache = null;
}

function listSessionsForPromptHistory(workspace?: string): PromptHistorySessionInfo[] {
  const dir = sessionsDir();
  if (!existsSync(dir)) return [];

  let sessions: PromptHistorySessionInfo[] = [];
  const now = Date.now();

  if (globalPromptHistoryCache && now - globalPromptHistoryCache.lastUpdated < CACHE_TTL_MS) {
    sessions = globalPromptHistoryCache.sessions;
  } else {
    try {
      const files = readdirSync(dir).filter(
        (f) => f.endsWith(".jsonl") && !f.endsWith(".events.jsonl"),
      );
      sessions = files.flatMap((file) => {
        const path = join(dir, file);
        const name = file.replace(/\.jsonl$/, "");
        const meta = loadSessionMeta(name);
        const stat = statSync(path);
        return [
          {
            name,
            path,
            mtime: stat.mtime,
            workspace: meta.workspace,
          },
        ];
      });
      sessions.sort((a, b) => b.mtime.getTime() - a.mtime.getTime());
      globalPromptHistoryCache = {
        sessions,
        lastUpdated: now,
      };
    } catch {
      return [];
    }
  }

  if (workspace) {
    const want = normalizeWorkspace(workspace);
    return sessions.filter((s) => {
      if (typeof s.workspace === "string") {
        return normalizeWorkspace(s.workspace) === want;
      }
      const legacyPrefix = legacySessionPrefixForWorkspace(workspace);
      return s.name.startsWith(legacyPrefix);
    });
  }

  return sessions;
}

export function promptHistoryStep(opts: PromptHistoryStepOptions): PromptHistoryEntry | null {
  const sessions = listSessionsForPromptHistory(opts.workspace);
  if (sessions.length === 0) return null;
  if (opts.direction === "newer" && !opts.cursor) return null;

  const cursorSession = opts.cursor?.sessionName;
  const startSession = cursorSession ?? opts.startSessionName ?? null;
  const foundStart = startSession
    ? sessions.findIndex((session) => session.name === sanitizeName(startSession))
    : -1;
  const startIndex =
    foundStart >= 0 ? foundStart : opts.direction === "older" ? 0 : sessions.length - 1;
  const stopIndex = opts.stopSessionName
    ? sessions.findIndex((session) => session.name === sanitizeName(opts.stopSessionName!))
    : -1;

  for (let offset = 0; offset < sessions.length; offset++) {
    const sessionIndex = opts.direction === "older" ? startIndex + offset : startIndex - offset;
    if (sessionIndex < 0 || sessionIndex >= sessions.length) break;
    if (opts.direction === "newer" && stopIndex >= 0 && sessionIndex < stopIndex) break;

    const session = sessions[sessionIndex];
    if (!session) continue;
    const messages = loadSessionMessages(session.name);
    const cursorIndex =
      offset === 0 && opts.cursor?.sessionName === session.name
        ? opts.cursor.messageIndex
        : undefined;
    const entry = findPromptHistoryEntryInMessages({
      sessionName: session.name,
      messages,
      direction: opts.direction,
      cursorIndex,
    });
    if (entry) return entry;
  }

  return null;
}

function findPromptHistoryEntryInMessages({
  sessionName,
  messages,
  direction,
  cursorIndex,
}: {
  sessionName: string;
  messages: ChatMessage[];
  direction: PromptHistoryDirection;
  cursorIndex?: number;
}): PromptHistoryEntry | null {
  if (direction === "older") {
    const start =
      cursorIndex === undefined
        ? messages.length - 1
        : Math.min(cursorIndex - 1, messages.length - 1);
    for (let i = start; i >= 0; i--) {
      const value = promptHistoryValue(messages[i]);
      if (!value) continue;
      return { value, cursor: { sessionName, messageIndex: i } };
    }
    return null;
  }

  const start =
    cursorIndex === undefined ? 0 : Math.max(0, Math.min(cursorIndex + 1, messages.length));
  for (let i = start; i < messages.length; i++) {
    const value = promptHistoryValue(messages[i]);
    if (!value) continue;
    return { value, cursor: { sessionName, messageIndex: i } };
  }
  return null;
}

function promptHistoryValue(message: ChatMessage | undefined): string | null {
  if (!message || message.role !== "user") return null;
  const value = typeof message.content === "string" ? message.content.trim() : "";
  return value || null;
}

export function legacySessionPrefixForWorkspace(workspace: string): string {
  const normalized = normalizeWorkspace(workspace);
  const base =
    process.platform === "win32" ? win32Path.basename(normalized) : posixPath.basename(normalized);
  return `${sanitizeName(`code-${base}`)}-`;
}

export function patchSessionWorkspaceIfMissing(name: string, workspace: string): boolean {
  const meta = loadSessionMeta(name);
  if (typeof meta.workspace === "string") return false;
  const prefix = legacySessionPrefixForWorkspace(workspace);
  if (!sanitizeName(name).startsWith(prefix)) return false;
  patchSessionMeta(name, { workspace });
  return true;
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
  invalidatePromptHistoryCache();
  return next;
}

/** SHA-256[:16] of a system-prompt string — used to detect REASONIX.md changes on resume (#2212). */
export function hashSystemPrompt(system: string): string {
  return createHash("sha256").update(system).digest("hex").slice(0, 16);
}

/** Persist the current system-prompt fingerprint so resume can detect changes. */
export function saveSessionSystemFingerprint(name: string, system: string): void {
  patchSessionMeta(name, { systemFingerprint: hashSystemPrompt(system) });
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
  invalidatePromptHistoryCache();
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
    invalidatePromptHistoryCache();
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
  const tmp = `${path}.${randomBytes(8).toString("hex")}.tmp`;
  if (existsSync(path) && statSync(path).size > 0) {
    const backup = sessionBackupPath(path);
    copyFileSync(path, backup);
    chmodPrivate(backup);
  }
  atomicWriteSync(path, body ? `${body}\n` : "", tmp);
  invalidatePromptHistoryCache();
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

/** Byte-scan for `\n` — avoids the UTF-8 decode + regex split + per-line filter the previous implementation paid on every list. ~10× faster on multi-MB jsonls. */
function countLines(path: string): number {
  try {
    const buf = readFileSync(path);
    let count = 0;
    for (let i = 0; i < buf.length; i++) {
      if (buf[i] === 0x0a) count++;
    }
    // appendSessionMessage always writes a trailing newline, but a
    // hand-edited file may end without one — account for the dangling line.
    if (buf.length > 0 && buf[buf.length - 1] !== 0x0a) count++;
    return count;
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

// Async variants — non-blocking counterparts for server API handlers.

async function countLinesAsync(path: string): Promise<number> {
  try {
    const buf = await readFile(path);
    let count = 0;
    for (let i = 0; i < buf.length; i++) {
      if (buf[i] === 0x0a) count++;
    }
    if (buf.length > 0 && buf[buf.length - 1] !== 0x0a) count++;
    return count;
  } catch {
    return 0;
  }
}

async function chmodPrivateAsync(path: string): Promise<void> {
  try {
    await chmod(path, 0o600);
  } catch {
    /* chmod not supported */
  }
}

export async function loadSessionMetaAsync(name: string): Promise<SessionMeta> {
  const p = metaPath(name);
  try {
    const raw = JSON.parse(await readFile(p, "utf8")) as SessionMeta;
    return raw && typeof raw === "object" ? raw : {};
  } catch {
    return {};
  }
}

export async function patchSessionMetaAsync(
  name: string,
  patch: Partial<SessionMeta>,
): Promise<SessionMeta> {
  const cur = await loadSessionMetaAsync(name);
  const next: SessionMeta = { ...cur, ...patch };
  const p = metaPath(name);
  await mkdir(dirname(p), { recursive: true });
  await writeFile(p, JSON.stringify(next), "utf8");
  await chmodPrivateAsync(p);
  return next;
}

async function readSessionMessagesAsync(
  path: string,
): Promise<{ messages: ChatMessage[]; hadContent: boolean } | null> {
  let raw: string;
  try {
    raw = await readFile(path, "utf8");
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

export async function loadSessionMessagesAsync(name: string): Promise<ChatMessage[]> {
  const path = sessionPath(name);
  const live = await readSessionMessagesAsync(path);
  if (live && (live.messages.length > 0 || !live.hadContent)) return live.messages;
  const backup = await readSessionMessagesAsync(sessionBackupPath(path));
  return backup?.messages ?? live?.messages ?? [];
}

export async function findSessionsByPrefixAsync(prefix: string): Promise<string[]> {
  const dir = sessionsDir();
  try {
    const files = (await readdir(dir))
      .filter((f) => f.endsWith(".jsonl") && !f.endsWith(".events.jsonl") && f.startsWith(prefix))
      .sort()
      .reverse();
    return files.map((f) => f.replace(/\.jsonl$/, ""));
  } catch {
    return [];
  }
}

export async function readTailMessagesAsync(path: string, count: number): Promise<ChatMessage[]> {
  try {
    const fd = await open(path, "r");
    try {
      const { size } = await fd.stat();
      if (size === 0) return [];
      const out: ChatMessage[] = [];
      let pos = size;
      let leftover = "";
      while (pos > 0 && out.length < count) {
        const chunkSize = Math.min(READ_CHUNK_SIZE, pos);
        pos -= chunkSize;
        const buf = Buffer.alloc(chunkSize);
        await fd.read(buf, 0, chunkSize, pos);
        const chunk = buf.toString("utf8") + leftover;
        const lines = chunk.split("\n");
        leftover = lines[0]!;
        for (let i = lines.length - 1; i >= 1 && out.length < count; i--) {
          const trimmed = lines[i]!.trim();
          if (!trimmed) continue;
          try {
            const msg = JSON.parse(trimmed) as ChatMessage;
            if (msg && typeof msg === "object" && "role" in msg) out.push(msg);
          } catch {
            /* skip malformed */
          }
        }
      }
      if (out.length < count && leftover.trim()) {
        try {
          const msg = JSON.parse(leftover.trim()) as ChatMessage;
          if (msg && typeof msg === "object" && "role" in msg) out.push(msg);
        } catch {
          /* skip */
        }
      }
      return out.reverse();
    } finally {
      await fd.close();
    }
  } catch {
    const raw = await readSessionMessagesAsync(path);
    return raw?.messages ?? [];
  }
}

export async function appendSessionMessageAsync(name: string, message: ChatMessage): Promise<void> {
  const path = sessionPath(name);
  await mkdir(dirname(path), { recursive: true });
  await appendFile(path, `${JSON.stringify(message)}\n`, "utf8");
  await chmodPrivateAsync(path);
}

export async function renameSessionAsync(oldName: string, newName: string): Promise<boolean> {
  const safeOld = sanitizeName(oldName);
  const safeNew = sanitizeName(newName);
  if (safeOld === safeNew) return false;
  const oldJsonl = sessionPath(oldName);
  const newJsonl = sessionPath(newName);
  try {
    await stat(oldJsonl);
  } catch {
    return false;
  }
  try {
    await stat(newJsonl);
    return false; // target already exists
  } catch {
    /* target doesn't exist — good */
  }
  await rename(oldJsonl, newJsonl);
  for (const ext of SESSION_SIDECAR_EXTS) {
    const oldP = oldJsonl.replace(/\.jsonl$/, ext);
    const newP = newJsonl.replace(/\.jsonl$/, ext);
    try {
      await rename(oldP, newP);
    } catch {
      /* sidecar rename failed — leave the jsonl rename in place */
    }
  }
  return true;
}

export async function deleteSessionAsync(name: string): Promise<boolean> {
  const path = sessionPath(name);
  try {
    await unlink(path);
    for (const ext of SESSION_SIDECAR_EXTS) {
      const sidecar = path.replace(/\.jsonl$/, ext);
      try {
        await unlink(sidecar);
      } catch {
        /* expected when the sidecar doesn't exist */
      }
    }
    return true;
  } catch {
    return false;
  }
}

export async function rewriteSessionAsync(name: string, messages: ChatMessage[]): Promise<void> {
  const path = sessionPath(name);
  await mkdir(dirname(path), { recursive: true });
  const body = messages.map((m) => JSON.stringify(m)).join("\n");
  const tmp = `${path}.${randomBytes(8).toString("hex")}.tmp`;
  try {
    const s = await stat(path);
    if (s.size > 0) {
      const backup = sessionBackupPath(path);
      await copyFile(path, backup);
      await chmodPrivateAsync(backup);
    }
  } catch {
    /* file doesn't exist yet — fine */
  }
  await atomicWrite(path, body ? `${body}\n` : "", tmp);
}

export async function listSessionsAsync(opts?: {
  workspaceFilter?: string;
  includeLegacyWorkspaceMatches?: boolean;
}): Promise<SessionInfo[]> {
  const dir = sessionsDir();
  const want = opts?.workspaceFilter ? normalizeWorkspace(opts.workspaceFilter) : null;
  const legacyPrefix =
    want && opts?.includeLegacyWorkspaceMatches
      ? legacySessionPrefixForWorkspace(opts.workspaceFilter!)
      : null;
  try {
    const files = (await readdir(dir)).filter(
      (f) => f.endsWith(".jsonl") && !f.endsWith(".events.jsonl"),
    );
    const results = await Promise.all(
      files.flatMap(async (file) => {
        const path = join(dir, file);
        const name = file.replace(/\.jsonl$/, "");
        const meta = await loadSessionMetaAsync(name);
        let workspaceStatus: SessionInfo["workspaceStatus"] | undefined;
        if (want !== null) {
          if (typeof meta.workspace === "string") {
            if (normalizeWorkspace(meta.workspace) !== want) return [];
            workspaceStatus = "matched";
          } else if (legacyPrefix && name.startsWith(legacyPrefix)) {
            workspaceStatus = "legacy_missing_meta";
          } else {
            return [];
          }
        }
        const s = await stat(path);
        const messageCount = await countLinesAsync(path);
        return [
          {
            name,
            path,
            size: s.size,
            messageCount,
            mtime: s.mtime,
            meta,
            workspaceStatus,
          } satisfies SessionInfo,
        ];
      }),
    );
    return results.flat().sort((a, b) => b.mtime.getTime() - a.mtime.getTime());
  } catch {
    return [];
  }
}

export async function listSessionsForWorkspaceAsync(workspace: string): Promise<SessionInfo[]> {
  return listSessionsAsync({
    workspaceFilter: workspace,
    includeLegacyWorkspaceMatches: true,
  });
}

export async function pruneStaleSessionsAsync(daysOld = 90): Promise<string[]> {
  const cutoff = Date.now() - daysOld * 24 * 60 * 60 * 1000;
  const deleted: string[] = [];
  for (const s of await listSessionsAsync()) {
    if (s.mtime.getTime() < cutoff) {
      if (await deleteSessionAsync(s.name)) deleted.push(s.name);
    }
  }
  return deleted;
}

export async function archiveSessionAsync(name: string): Promise<string | null> {
  const path = sessionPath(name);
  try {
    const s = await stat(path);
    if (s.size === 0) return null;
  } catch {
    return null;
  }
  for (let attempt = 0; attempt < 5; attempt++) {
    const target = `${name}__archive_${timestampSuffix()}${attempt > 0 ? `_${attempt}` : ""}`;
    if (await renameSessionAsync(name, target)) return target;
  }
  return null;
}
