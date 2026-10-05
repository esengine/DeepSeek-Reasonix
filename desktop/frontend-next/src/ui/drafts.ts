const PREFIX = "rx-studio-draft-v1:";
const MAX_DRAFT_BYTES = 32 * 1024;

export function draftKey(host: string, root: string, path: string): string {
  return path ? PREFIX + encodeURIComponent(JSON.stringify([host, root, path])) : "";
}

export function readDraft(key: string): string {
  if (!key) return "";
  try {
    const text = localStorage.getItem(key) ?? "";
    return new TextEncoder().encode(text).length <= MAX_DRAFT_BYTES ? text : "";
  } catch {
    return "";
  }
}

export function writeDraft(key: string, text: string): void {
  if (!key) return;
  try {
    if (!text) localStorage.removeItem(key);
    else if (new TextEncoder().encode(text).length <= MAX_DRAFT_BYTES) localStorage.setItem(key, text);
    else localStorage.removeItem(key);
  } catch {
    // Storage can be disabled or full. The composer still keeps its live draft.
  }
}

export function clearDraftForSession(host: string, path: string): void {
  try {
    for (let i = localStorage.length - 1; i >= 0; i--) {
      const key = localStorage.key(i);
      if (!key?.startsWith(PREFIX)) continue;
      try {
        const parts = JSON.parse(decodeURIComponent(key.slice(PREFIX.length)));
        if (Array.isArray(parts) && parts[0] === host && parts[2] === path) localStorage.removeItem(key);
      } catch {
        continue;
      }
    }
  } catch {
    // Clearing a draft must never block archiving its session.
  }
}
