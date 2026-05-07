/** JSONL append-only message log under `~/.reasonix/sessions/`; concurrent-write safe. */

import { execFileSync } from "node:child_process";
import {
  appendFileSync,
  chmodSync,
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
import { dirname, join } from "node:path";
import type { ChatMessage } from "../types.js";

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
  try {
    const raw = readFileSync(path, "utf8");
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
    return out;
  } catch {
    return [];
  }
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

/** Strict match — legacy sessions without meta.workspace are hidden; resume by name still works. */
export function listSessionsForWorkspace(workspace: string): SessionInfo[] {
  return listSessions().filter((s) => s.meta.workspace === workspace);
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
  for (const ext of [".events.jsonl", ".meta.json", ".pending.json", ".plan.json"]) {
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
    for (const ext of [".events.jsonl", ".pending.json", ".meta.json", ".plan.json"]) {
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

/** Non-atomic truncate+write window is acceptable — concurrent crash here = `/forget`. */
export function rewriteSession(name: string, messages: ChatMessage[]): void {
  const path = sessionPath(name);
  mkdirSync(dirname(path), { recursive: true });
  const body = messages.map((m) => JSON.stringify(m)).join("\n");
  writeFileSync(path, body ? `${body}\n` : "", "utf8");
  try {
    chmodSync(path, 0o600);
  } catch {
    /* chmod not supported */
  }
}

function countLines(path: string): number {
  try {
    const raw = readFileSync(path, "utf8");
    return raw.split(/\r?\n/).filter((l) => l.trim()).length;
  } catch {
    return 0;
  }
}
