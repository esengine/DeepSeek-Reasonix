import type { DirEntry } from "./types";

export function workspaceEntryPath(dir: string, entry: DirEntry): string {
  const prefix = dir === "" || dir.endsWith("/") ? dir : `${dir}/`;
  return prefix + entry.name + (entry.isDir ? "/" : "");
}

export function workspaceBasename(path: string): string {
  const parts = path.split("/").filter(Boolean);
  return parts[parts.length - 1] ?? "";
}

export function workspaceParentPath(path: string): string {
  return path.replace(/\/$/, "").split("/").filter(Boolean).slice(0, -1).join("/");
}

export function workspaceParentDirs(path: string): string[] {
  const parts = path.split("/").filter(Boolean);
  const dirs = [""];
  let current = "";
  for (let index = 0; index < parts.length - 1; index++) {
    current += `${parts[index]}/`;
    dirs.push(current);
  }
  return dirs;
}

export function workspaceTopLevelDirPath(path: string): string {
  const first = path.split("/").find(Boolean);
  return first ? `${first}/` : "";
}

export function workspaceShortCwd(cwd?: string): string {
  if (!cwd) return "";
  const parts = cwd.split("/").filter(Boolean);
  return parts.length <= 2 ? cwd : `…/${parts.slice(-2).join("/")}`;
}

export function workspaceFormatBytes(bytes: number): string {
  if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  if (bytes >= 1024) return `${Math.ceil(bytes / 1024)} KB`;
  return `${bytes} B`;
}

export function workspaceFormatCommitDate(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const month = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"][date.getMonth()];
  return `${String(date.getDate()).padStart(2, "0")} ${month} ${date.getFullYear()} ${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`;
}
