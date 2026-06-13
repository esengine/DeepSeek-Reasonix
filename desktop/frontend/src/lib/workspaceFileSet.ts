import type { DirEntry } from "./types";

// Build a Set of relative file paths from the workspace directory listing.
// Each entry key is "dir/name" for nested entries or "name" for root entries.
export function buildFileSet(entriesByDir: Record<string, DirEntry[]>): Set<string> {
  const set = new Set<string>();
  for (const [dir, entries] of Object.entries(entriesByDir)) {
    for (const entry of entries) {
      if (!entry.isDir) {
        set.add(dir ? `${dir}/${entry.name}` : entry.name);
      }
    }
  }
  return set;
}

// Build a Set of relative file paths from a flat string array (Go API response).
export function buildFileSetFromList(files: string[]): Set<string> {
  return new Set(files);
}

// Return a Map from basename to the set of full paths, for ambiguity detection.
// If a basename maps to exactly one path, it is "unique" and can be linked by
// short name. If it maps to more than one, only the full path is linkable.
export function buildBasenameIndex(
  fileSet: Set<string>,
): Map<string, string | null> {
  const index = new Map<string, string | null>();
  for (const path of fileSet) {
    const base = path.split("/").pop()!;
    const existing = index.get(base);
    if (existing === undefined) {
      index.set(base, path);
    } else if (existing !== null) {
      index.set(base, null); // ambiguous
    }
  }
  return index;
}
