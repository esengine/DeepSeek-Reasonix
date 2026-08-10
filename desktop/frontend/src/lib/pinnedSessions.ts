const KEY = "reasonix.pinned-sessions";
const CAP = 20;

function readAll(): string[] {
  if (typeof localStorage === "undefined") return [];
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return [];
    const value = JSON.parse(raw);
    if (!Array.isArray(value)) return [];
    return value.filter((entry): entry is string => typeof entry === "string");
  } catch {
    return [];
  }
}

function writeAll(list: string[]): void {
  if (typeof localStorage === "undefined") return;
  try {
    localStorage.setItem(KEY, JSON.stringify(list));
  } catch {
    return;
  }
}

export function toggle(path: string): boolean {
  const all = readAll();
  const index = all.indexOf(path);
  if (index >= 0) {
    all.splice(index, 1);
    writeAll(all);
    return false;
  }
  all.unshift(path);
  if (all.length > CAP) all.length = CAP;
  writeAll(all);
  return true;
}

export function ordered(existing: ReadonlyArray<{ path: string }>): string[] {
  const live = new Set(existing.map((session) => session.path));
  return readAll().filter((path) => live.has(path));
}
