import type { DirEntry } from "./types";

export interface WorkspaceSearchRow {
  path: string;
  entry: DirEntry;
}

function basename(path: string): string {
  const parts = path.split("/").filter(Boolean);
  return parts[parts.length - 1] || path;
}

function treeSearchPath(entry: DirEntry): string {
  const path = (entry.path || entry.name).replace(/\\/g, "/");
  if (!entry.isDir || path.endsWith("/")) return path;
  return path + "/";
}

export function mergeWorkspaceSearchResults(rows: WorkspaceSearchRow[], results: DirEntry[] | null): WorkspaceSearchRow[] {
  if (!results || results.length === 0) return rows;
  const merged = [...rows];
  const seen = new Set(rows.map((row) => row.path));
  for (const result of results) {
    const path = treeSearchPath(result);
    if (seen.has(path)) continue;
    merged.push({ path, entry: { ...result, name: result.displayName || basename(path) } });
    seen.add(path);
  }
  return merged;
}
