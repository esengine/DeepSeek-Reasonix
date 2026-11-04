const KEPT = 6;
const key = (root: string) => `workbench.recent:${root}`;

export function readRecentFiles(root: string): string[] {
  try {
    const raw = JSON.parse(localStorage.getItem(key(root)) ?? "[]");
    return Array.isArray(raw) ? raw.filter((x): x is string => typeof x === "string") : [];
  } catch {
    return [];
  }
}

export function rememberFile(root: string, path: string): string[] {
  const next = [path, ...readRecentFiles(root).filter((x) => x !== path)].slice(0, KEPT);
  try {
    localStorage.setItem(key(root), JSON.stringify(next));
  } catch {}
  return next;
}
