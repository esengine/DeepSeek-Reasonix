import { useEffect, type DependencyList } from "react";

const SHORTCUTS_STORAGE_KEY = "reasonix.customShortcuts";

export type ShortcutAction =
  | "shortcuts.palette"
  | "shortcuts.closeTab"
  | "shortcuts.newSession"
  | "shortcuts.settings"
  | "shortcuts.shellExpand"
  | "shortcuts.nextUnread"
  | "shortcuts.yoloToggle"
  | "shortcuts.textSizeIncrease"
  | "shortcuts.textSizeDecrease"
  | "shortcuts.textSizeReset";

const SHORTCUTS_CHANGED_EVENT = "reasonix:shortcuts-changed";

let cachedCustomKeys: Record<string, string> | null = null;

/**
 * Load custom shortcuts from localStorage, using a module-level cache.
 * The cache is invalidated when:
 *   1. `storage` event fires (another tab changed it)
 *   2. `reasonix:shortcuts-changed` custom event fires (same tab)
 *   3. `invalidateShortcutsCache()` is called explicitly
 */
export function loadCustomShortcuts(): Record<string, string> {
  if (cachedCustomKeys !== null) return cachedCustomKeys;
  try {
    const raw = localStorage.getItem(SHORTCUTS_STORAGE_KEY);
    cachedCustomKeys = raw ? (JSON.parse(raw) as Record<string, string>) : {};
  } catch {
    cachedCustomKeys = {};
  }
  return cachedCustomKeys;
}

/** Force the module cache to reload on next access. Call after saving. */
export function invalidateShortcutsCache(): void {
  cachedCustomKeys = null;
}

// Listen for cross-tab changes and custom invalidation events.
if (typeof window !== "undefined") {
  window.addEventListener("storage", (e: StorageEvent) => {
    if (e.key === SHORTCUTS_STORAGE_KEY) cachedCustomKeys = null;
  });
  window.addEventListener(SHORTCUTS_CHANGED_EVENT, () => {
    cachedCustomKeys = null;
  });
}

/** Dispatch a custom event so the same tab can invalidate the cache after saving. */
export function notifyShortcutsChanged(): void {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new CustomEvent(SHORTCUTS_CHANGED_EVENT));
}

/**
 * Format a KeyboardEvent into a canonical combo string (e.g. "⌘K", "Ctrl+Shift+P").
 * Used both by the recording UI and the dispatch side so they stay in sync.
 */
export function formatKeyCombo(e: { metaKey: boolean; ctrlKey: boolean; altKey: boolean; shiftKey: boolean; key: string }, platform: "darwin" | "win"): string {
  if (["Meta", "Control", "Alt", "Shift"].includes(e.key)) return "";
  const parts: string[] = [];
  if (e.metaKey) parts.push(platform === "darwin" ? "⌘" : "Win");
  if (e.ctrlKey) parts.push("Ctrl");
  if (e.altKey) parts.push(platform === "darwin" ? "⌥" : "Alt");
  if (e.shiftKey) parts.push("⇧");
  const key = e.key.length === 1 ? e.key.toUpperCase() : e.key;
  parts.push(key);
  return parts.join(platform === "darwin" ? "" : "+");
}

/**
 * Get the resolved key combo for an action, respecting user customizations.
 * Falls back to the default platform-specific combo.
 */
export const SHORTCUT_DEFAULTS: Record<ShortcutAction, { mac: string; win: string }> = {
  "shortcuts.palette": { mac: "⌘K", win: "Ctrl+K" },
  "shortcuts.closeTab": { mac: "⌘W", win: "Ctrl+W" },
  "shortcuts.newSession": { mac: "⌘N", win: "Ctrl+N" },
  "shortcuts.settings": { mac: "⌘,", win: "Ctrl+," },
  "shortcuts.shellExpand": { mac: "⌘B", win: "Ctrl+B" },
  "shortcuts.yoloToggle": { mac: "⌘Y", win: "Ctrl+Y" },
  "shortcuts.nextUnread": { mac: "⌘G", win: "Ctrl+G" },
  "shortcuts.textSizeIncrease": { mac: "⌘=", win: "Ctrl+=" },
  "shortcuts.textSizeDecrease": { mac: "⌘-", win: "Ctrl+-" },
  "shortcuts.textSizeReset": { mac: "⌘0", win: "Ctrl+0" },
};

function getShortcutKey(action: ShortcutAction, platform: "darwin" | "win"): string {
  const custom = loadCustomShortcuts();
  const customKey = custom[action];
  if (customKey) return customKey;

  return platform === "darwin" ? SHORTCUT_DEFAULTS[action].mac : SHORTCUT_DEFAULTS[action].win;
}

/**
 * Check if a keydown event matches the custom or default shortcut for an action.
 */
export function matchesShortcut(e: { metaKey: boolean; ctrlKey: boolean; altKey: boolean; shiftKey: boolean; key: string }, action: ShortcutAction, platform: "darwin" | "win"): boolean {
  const combo = formatKeyCombo(e, platform);
  if (!combo) return false;
  return combo === getShortcutKey(action, platform);
}

/**
 * Register a global keyboard shortcut handler that respects the user's custom
 * key bindings configured in the settings panel. Pass `enabled=false` to
 * temporarily suppress the handler (e.g. when the tab bar is hidden).
 *
 * The handler is mounted on `document` without capture; for `window` + capture
 * semantics call `matchesShortcut` manually inside your own `useEffect`.
 */
export function useGlobalHotkey(
  action: ShortcutAction,
  handler: (e: globalThis.KeyboardEvent) => void,
  deps: DependencyList = [],
  enabled: boolean = true,
): void {
  useEffect(() => {
    if (!enabled) return;
    const platform: "darwin" | "win" = navigator.platform.startsWith("Mac") ? "darwin" : "win";
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (matchesShortcut(e, action, platform)) {
        handler(e);
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [action, handler, enabled, ...deps]);
}
