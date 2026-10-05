// Which pane this tab is showing survives a reload of the tab, and only that:
// the kernel keeps every pane open across it, but no answer it holds says which
// one a given window was looking at. Per tab, so two windows never trade theirs.
const KEY = "rx-active-pane";

export function savedActivePane(): string {
  try {
    return sessionStorage.getItem(KEY) ?? "";
  } catch {
    return "";
  }
}

export function rememberActivePane(id: string) {
  if (!id) return;
  try {
    sessionStorage.setItem(KEY, id);
  } catch {
    // Storage refused: the next load falls back to the first open pane.
  }
}
