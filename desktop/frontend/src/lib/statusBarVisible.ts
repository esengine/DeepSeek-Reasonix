const KEY = "reasonix-status-bar-visible";
const EVENT = "reasonix:status-bar-visible";

export function getStatusBarVisible(): boolean {
  if (typeof localStorage === "undefined") return true;
  const stored = localStorage.getItem(KEY);
  if (stored === null) return true;
  return stored !== "false";
}

export function setStatusBarVisible(visible: boolean): void {
  localStorage.setItem(KEY, visible ? "true" : "false");
  window.dispatchEvent(new CustomEvent(EVENT, { detail: visible }));
}

export function onStatusBarVisibleChange(cb: (visible: boolean) => void): () => void {
  const handler = (e: Event) => cb((e as CustomEvent).detail as boolean);
  window.addEventListener(EVENT, handler);
  return () => window.removeEventListener(EVENT, handler);
}
