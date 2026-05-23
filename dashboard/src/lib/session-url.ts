const SESSION_PARAM = "session";

export function readSessionFromUrl(): string | null {
  if (typeof window === "undefined") return null;
  const raw = new URLSearchParams(window.location.search).get(SESSION_PARAM);
  if (!raw) return null;
  const trimmed = raw.trim();
  return trimmed.length > 0 ? trimmed : null;
}

export function writeSessionToUrl(name: string | null): void {
  if (typeof window === "undefined") return;
  const url = new URL(window.location.href);
  if (name && name.trim().length > 0) {
    url.searchParams.set(SESSION_PARAM, name);
  } else {
    url.searchParams.delete(SESSION_PARAM);
  }
  if (url.href === window.location.href) return;
  window.history.replaceState(null, "", url.href);
}
