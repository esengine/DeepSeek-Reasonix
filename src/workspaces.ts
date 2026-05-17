import { statSync } from "node:fs";
import { resolve } from "node:path";
import { loadRecentWorkspaces, loadWorkspaceDir, pushRecentWorkspace } from "./config.js";
import { listSessions, normalizeWorkspace } from "./memory/session.js";

export interface WorkspaceInfo {
  path: string;
  current: boolean;
  sessions: number;
  lastActive?: Date;
}

interface WorkspaceStats {
  sessions: number;
  lastActive?: Date;
}

export function listKnownWorkspaces(currentRoot: string, configPath?: string): WorkspaceInfo[] {
  const { stats, pathByKey } = collectSessionWorkspaceStats();
  const out: WorkspaceInfo[] = [];
  const seen = new Map<string, WorkspaceInfo>();

  const add = (raw: string | undefined, current = false) => {
    if (typeof raw !== "string" || raw.trim().length === 0) return;
    const path = resolve(raw);
    if (!isDirectory(path)) return;
    const key = normalizeWorkspace(path);
    const found = seen.get(key);
    const s = stats.get(key);
    if (found) {
      found.current ||= current;
      if (s) {
        found.sessions = s.sessions;
        found.lastActive = s.lastActive;
      }
      return;
    }
    const info: WorkspaceInfo = {
      path,
      current,
      sessions: s?.sessions ?? 0,
      lastActive: s?.lastActive,
    };
    seen.set(key, info);
    out.push(info);
  };

  add(currentRoot, true);
  add(loadWorkspaceDir(configPath));
  for (const p of loadRecentWorkspaces(configPath)) add(p);
  for (const [key, s] of stats) {
    const path = pathByKey.get(key);
    if (!path) continue;
    add(path);
  }

  return out;
}

export function rememberWorkspace(path: string, configPath?: string): string {
  const resolved = resolve(path);
  pushRecentWorkspace(resolved, configPath);
  return resolved;
}

function collectSessionWorkspaceStats(): {
  stats: Map<string, WorkspaceStats>;
  pathByKey: Map<string, string>;
} {
  const stats = new Map<string, WorkspaceStats>();
  const pathByKey = new Map<string, string>();
  for (const session of listSessions()) {
    const raw = session.meta.workspace;
    if (typeof raw !== "string" || raw.trim().length === 0) continue;
    const path = resolve(raw);
    if (!isDirectory(path)) continue;
    const key = normalizeWorkspace(path);
    pathByKey.set(key, path);
    const cur = stats.get(key) ?? { sessions: 0 };
    cur.sessions += 1;
    if (!cur.lastActive || session.mtime.getTime() > cur.lastActive.getTime()) {
      cur.lastActive = session.mtime;
    }
    stats.set(key, cur);
  }
  return { stats, pathByKey };
}

function isDirectory(path: string): boolean {
  try {
    return statSync(path).isDirectory();
  } catch {
    return false;
  }
}
