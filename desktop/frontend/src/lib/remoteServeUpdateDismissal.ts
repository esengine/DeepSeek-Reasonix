// Per-serve dismissal memory for the remote Serve update banner. A dismissal
// is keyed by host, workspace, and the serve version it was shown for, so a
// replaced serve (new version) makes the banner eligible again.
const STORAGE_KEY = "remote.serveUpdate.dismissed";
const MAX_ENTRIES = 200;

// In-memory twin of the persisted set: when localStorage is unavailable
// (private mode, quota), Ignore still holds for the application session
// instead of resetting on the next banner remount.
const sessionDismissed = new Set<string>();

function keyFor(hostId: string, workspace: string, serveVersion: string): string {
  return `${hostId}|${workspace}|${serveVersion}`;
}

function readSet(): Set<string> {
  // Stored entries first, session entries after: the newest key must sit at
  // the end so the persisted slice drops the oldest entry, not the new one.
  const merged = new Set<string>();
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    const list = raw ? (JSON.parse(raw) as unknown) : [];
    for (const entry of Array.isArray(list) ? list.map(String) : []) {
      merged.add(entry);
    }
  } catch {
    // Storage unreadable: the session set still applies.
  }
  for (const entry of sessionDismissed) {
    merged.add(entry);
  }
  return merged;
}

export function isRemoteServeUpdateDismissed(hostId: string, workspace: string, serveVersion: string): boolean {
  return readSet().has(keyFor(hostId, workspace, serveVersion));
}

export function dismissRemoteServeUpdate(hostId: string, workspace: string, serveVersion: string): void {
  const key = keyFor(hostId, workspace, serveVersion);
  sessionDismissed.add(key);
  if (sessionDismissed.size > MAX_ENTRIES) {
    sessionDismissed.delete(sessionDismissed.values().next().value as string);
  }
  try {
    const set = readSet();
    set.add(key);
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify([...set].slice(-MAX_ENTRIES)));
  } catch {
    // Storage unavailable (private mode): the session set keeps the dismissal.
  }
}
