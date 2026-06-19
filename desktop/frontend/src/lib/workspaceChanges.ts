import type { Translator } from "./i18n";
import type { DictKey } from "../locales/en";
import type { WorkspaceChangeRecord, WorkspaceChangesView, WorkspaceChangeView } from "./types";

export interface WorkspaceChangeRow {
  key: string;
  path: string;
  detail: string;
  badges: string[];
  time?: number;
}

function normalizedStatus(status?: string): string {
  return (status ?? "").trim();
}

export function workspaceGitStatusLabelKey(status?: string): DictKey | null {
  const s = normalizedStatus(status);
  if (!s) return null;
  if (s === "??" || s.toLowerCase() === "untracked") return "workspace.gitStatusUntracked";
  if (s.includes("U")) return "workspace.gitStatusUnmerged";
  if (s.includes("R")) return "workspace.gitStatusRenamed";
  if (s.includes("C")) return "workspace.gitStatusCopied";
  if (s.includes("D")) return "workspace.gitStatusDeleted";
  if (s.includes("A")) return "workspace.gitStatusAdded";
  if (s.includes("M") || s.toLowerCase() === "modified") return "workspace.gitStatusModified";
  return null;
}

export function workspaceGitStatusLabel(status: string | undefined, t: Translator): string {
  const key = workspaceGitStatusLabelKey(status);
  return key ? t(key) : normalizedStatus(status);
}

function turnBadge(turn: number | undefined, t: Translator): string {
  if (typeof turn !== "number" || !Number.isFinite(turn)) return "";
  return t("workspace.turnBadge", { turn: Math.max(1, Math.floor(turn) + 1) });
}

function recordRow(record: WorkspaceChangeRecord, t: Translator): WorkspaceChangeRow {
  return {
    key: record.key || `${record.turn}:${record.path}`,
    path: record.path,
    detail: record.prompt ?? "",
    badges: [turnBadge(record.turn, t), workspaceGitStatusLabel(record.gitStatus, t)].filter(Boolean),
    time: record.time,
  };
}

function fileFallbackRow(file: WorkspaceChangeView, t: Translator): WorkspaceChangeRow {
  const turns = Array.isArray(file.turns) ? file.turns : [];
  const turnLabel = turns.length === 1 ? turnBadge(turns[0], t) : "";
  return {
    key: file.path,
    path: file.path,
    detail: file.latestPrompt ?? "",
    badges: [turnLabel, workspaceGitStatusLabel(file.gitStatus, t)].filter(Boolean),
    time: file.latestTime,
  };
}

function gitOnlyRow(file: WorkspaceChangeView, t: Translator): WorkspaceChangeRow {
  return {
    key: `git:${file.path}`,
    path: file.path,
    detail: "",
    badges: [workspaceGitStatusLabel(file.gitStatus, t)].filter(Boolean),
    time: file.latestTime,
  };
}

export function workspaceChangeRows(view: WorkspaceChangesView | null | undefined, t: Translator): WorkspaceChangeRow[] {
  if (!view) return [];
  const files = Array.isArray(view.files) ? view.files : [];
  const records = Array.isArray(view.records) ? view.records : [];
  const rows = records.length > 0
    ? records.map((record) => recordRow(record, t))
    : files.filter((file) => file.sources?.includes("session")).map((file) => fileFallbackRow(file, t));

  for (const file of files) {
    if (!file.sources?.includes("session") && file.sources?.includes("git")) {
      rows.push(gitOnlyRow(file, t));
    }
  }

  return rows.sort((a, b) => {
    const at = a.time ?? 0;
    const bt = b.time ?? 0;
    if (at !== bt) return bt - at;
    return a.path.localeCompare(b.path);
  });
}

export function workspaceVisibleChangeCount(view: WorkspaceChangesView | null | undefined): number {
  if (!view) return 0;
  const files = Array.isArray(view.files) ? view.files : [];
  const records = Array.isArray(view.records) ? view.records : [];
  if (records.length > 0) {
    return records.length + files.filter((file) => !file.sources?.includes("session") && file.sources?.includes("git")).length;
  }
  return files.reduce((count, file) => {
    if (!file.sources?.includes("session")) return file.sources?.includes("git") ? count + 1 : count;
    const turns = Array.isArray(file.turns) ? file.turns.length : 0;
    return count + Math.max(1, turns);
  }, 0);
}
