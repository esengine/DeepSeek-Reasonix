import { useEffect, useState, type DependencyList } from "react";
import type { DictKey } from "./i18n";

export type ShortcutPlatform = "darwin" | "windows" | "linux";

export type ShortcutAction =
  | "app.newSession"
  | "commandPalette.open"
  | "composer.newline"
  | "composer.send"
  | "selection.addToChat"
  | "settings.open"
  | "tab.close"
  | "shell.toggle"
  | "sidebar.toggle"
  | "textSize.increase"
  | "textSize.decrease"
  | "textSize.reset"
  | "toolApproval.yolo"
  | "shortcuts.show"
  | "topic.goto.1"
  | "topic.goto.2"
  | "topic.goto.3"
  | "topic.goto.4"
  | "topic.goto.5"
  | "topic.goto.6"
  | "topic.goto.7"
  | "topic.goto.8"
  | "topic.goto.9";

type KeyboardShortcutEvent = Pick<globalThis.KeyboardEvent, "key"> &
  Partial<Pick<globalThis.KeyboardEvent, "ctrlKey" | "metaKey" | "altKey" | "shiftKey" | "target">>;

export type ShortcutCombo = {
  key: string;
  ctrl?: boolean;
  meta?: boolean;
  alt?: boolean;
  shift?: boolean;
};

export type ShortcutSection = "global" | "session" | "view" | "tools" | "help";

export type ShortcutDefinition = {
  action: ShortcutAction;
  section: ShortcutSection;
  labelKey: DictKey;
  descriptionKey: DictKey;
  defaults: Record<ShortcutPlatform, ShortcutCombo>;
  aliases?: Partial<Record<ShortcutPlatform, ShortcutCombo[]>>;
  preventDefault?: boolean;
  allowInEditable?: boolean;
  configurable?: boolean;
  allowedKeys?: readonly string[];
};

const SHORTCUTS_STORAGE_KEY = "reasonix.customShortcuts";
const SHORTCUTS_CHANGED_EVENT = "reasonix:shortcuts-changed";

export const SHORTCUT_DEFINITIONS: readonly ShortcutDefinition[] = [
  {
    action: "app.newSession",
    section: "session",
    labelKey: "shortcuts.action.newSession",
    descriptionKey: "shortcuts.desc.newSession",
    defaults: modCombo("n"),
    preventDefault: true,
  },
  {
    action: "commandPalette.open",
    section: "global",
    labelKey: "shortcuts.action.commandPalette",
    descriptionKey: "shortcuts.desc.commandPalette",
    defaults: modCombo("k"),
    preventDefault: true,
    allowInEditable: true,
  },
  {
    action: "settings.open",
    section: "global",
    labelKey: "shortcuts.action.settings",
    descriptionKey: "shortcuts.desc.settings",
    defaults: modCombo(","),
    preventDefault: true,
  },
  {
    action: "tab.close",
    section: "session",
    labelKey: "shortcuts.action.closeTab",
    descriptionKey: "shortcuts.desc.closeTab",
    defaults: modCombo("w"),
    preventDefault: true,
  },
  // composer.send / composer.newline are handled inside the composer's own
  // keydown path (see composerKeyboard.ts), not via useGlobalShortcut. That
  // path also owns backward compatibility for the default send behavior.
  {
    action: "composer.send",
    section: "session",
    labelKey: "shortcuts.action.composerSend",
    descriptionKey: "shortcuts.desc.composerSend",
    defaults: allPlatforms({ key: "Enter" }),
    allowInEditable: true,
    allowedKeys: ["Enter"],
  },
  {
    action: "composer.newline",
    section: "session",
    labelKey: "shortcuts.action.composerNewline",
    descriptionKey: "shortcuts.desc.composerNewline",
    defaults: allPlatforms({ key: "Enter", shift: true }),
    allowInEditable: true,
    allowedKeys: ["Enter"],
  },
  {
    action: "selection.addToChat",
    section: "session",
    labelKey: "shortcuts.action.addSelectionToChat",
    descriptionKey: "shortcuts.desc.addSelectionToChat",
    defaults: modCombo("l"),
    preventDefault: true,
    // The handler only arms while the transcript selection action is visible,
    // so firing from an editable target (composer focus) is safe and expected.
    allowInEditable: true,
  },
  {
    action: "shell.toggle",
    section: "view",
    labelKey: "shortcuts.action.shellToggle",
    descriptionKey: "shortcuts.desc.shellToggle",
    defaults: {
      darwin: { key: "b", meta: true, shift: true },
      windows: { key: "b", ctrl: true, shift: true },
      linux: { key: "b", ctrl: true, shift: true },
    },
    preventDefault: true,
  },
  {
    action: "sidebar.toggle",
    section: "view",
    labelKey: "shortcuts.action.sidebarToggle",
    descriptionKey: "shortcuts.desc.sidebarToggle",
    defaults: modCombo("b"),
    preventDefault: true,
  },
  {
    action: "textSize.increase",
    section: "view",
    labelKey: "shortcuts.action.textSizeIncrease",
    descriptionKey: "shortcuts.desc.textSizeIncrease",
    defaults: modCombo("="),
    aliases: {
      darwin: [{ key: "+", meta: true, shift: true }],
      windows: [{ key: "+", ctrl: true, shift: true }],
      linux: [{ key: "+", ctrl: true, shift: true }],
    },
    preventDefault: true,
  },
  {
    action: "textSize.decrease",
    section: "view",
    labelKey: "shortcuts.action.textSizeDecrease",
    descriptionKey: "shortcuts.desc.textSizeDecrease",
    defaults: modCombo("-"),
    preventDefault: true,
  },
  {
    action: "textSize.reset",
    section: "view",
    labelKey: "shortcuts.action.textSizeReset",
    descriptionKey: "shortcuts.desc.textSizeReset",
    defaults: modCombo("0"),
    preventDefault: true,
  },
  {
    action: "toolApproval.yolo",
    section: "tools",
    labelKey: "shortcuts.action.yoloToggle",
    descriptionKey: "shortcuts.desc.yoloToggle",
    defaults: modCombo("y"),
    preventDefault: true,
    allowInEditable: true,
  },
  {
    action: "shortcuts.show",
    section: "help",
    labelKey: "shortcuts.action.showShortcuts",
    descriptionKey: "shortcuts.desc.showShortcuts",
    defaults: allPlatforms({ key: "?", shift: true }),
    preventDefault: true,
  },
  {
    action: "topic.goto.1",
    section: "session",
    labelKey: "shortcuts.action.topicGoto1",
    descriptionKey: "shortcuts.desc.topicGoto",
    defaults: modCombo("1"),
    preventDefault: true,
    configurable: false,
  },
  {
    action: "topic.goto.2",
    section: "session",
    labelKey: "shortcuts.action.topicGoto2",
    descriptionKey: "shortcuts.desc.topicGoto",
    defaults: modCombo("2"),
    preventDefault: true,
    configurable: false,
  },
  {
    action: "topic.goto.3",
    section: "session",
    labelKey: "shortcuts.action.topicGoto3",
    descriptionKey: "shortcuts.desc.topicGoto",
    defaults: modCombo("3"),
    preventDefault: true,
    configurable: false,
  },
  {
    action: "topic.goto.4",
    section: "session",
    labelKey: "shortcuts.action.topicGoto4",
    descriptionKey: "shortcuts.desc.topicGoto",
    defaults: modCombo("4"),
    preventDefault: true,
    configurable: false,
  },
  {
    action: "topic.goto.5",
    section: "session",
    labelKey: "shortcuts.action.topicGoto5",
    descriptionKey: "shortcuts.desc.topicGoto",
    defaults: modCombo("5"),
    preventDefault: true,
    configurable: false,
  },
  {
    action: "topic.goto.6",
    section: "session",
    labelKey: "shortcuts.action.topicGoto6",
    descriptionKey: "shortcuts.desc.topicGoto",
    defaults: modCombo("6"),
    preventDefault: true,
    configurable: false,
  },
  {
    action: "topic.goto.7",
    section: "session",
    labelKey: "shortcuts.action.topicGoto7",
    descriptionKey: "shortcuts.desc.topicGoto",
    defaults: modCombo("7"),
    preventDefault: true,
    configurable: false,
  },
  {
    action: "topic.goto.8",
    section: "session",
    labelKey: "shortcuts.action.topicGoto8",
    descriptionKey: "shortcuts.desc.topicGoto",
    defaults: modCombo("8"),
    preventDefault: true,
    configurable: false,
  },
  {
    action: "topic.goto.9",
    section: "session",
    labelKey: "shortcuts.action.topicGoto9",
    descriptionKey: "shortcuts.desc.topicGoto",
    defaults: modCombo("9"),
    preventDefault: true,
    configurable: false,
  },
] as const;

let cachedCustomShortcuts: Partial<Record<ShortcutAction, ShortcutCombo>> | null = null;

if (typeof window !== "undefined") {
  window.addEventListener("storage", (event) => {
    if (event.key === SHORTCUTS_STORAGE_KEY) cachedCustomShortcuts = null;
  });
}

function allPlatforms(combo: ShortcutCombo): Record<ShortcutPlatform, ShortcutCombo> {
  return {
    darwin: combo,
    windows: combo,
    linux: combo,
  };
}

function modCombo(key: string): Record<ShortcutPlatform, ShortcutCombo> {
  return {
    darwin: { key, meta: true },
    windows: { key, ctrl: true },
    linux: { key, ctrl: true },
  };
}

export function detectShortcutPlatform(): ShortcutPlatform {
  if (typeof navigator === "undefined") return "linux";
  const platform = navigator.platform || "";
  const userAgent = navigator.userAgent || "";
  if (/Mac|iPhone|iPad/.test(platform) || /Mac|iPhone|iPad/.test(userAgent)) return "darwin";
  if (/Win/.test(platform) || /Windows/.test(userAgent)) return "windows";
  return "linux";
}

export function shortcutDefinitions(): readonly ShortcutDefinition[] {
  return SHORTCUT_DEFINITIONS;
}

export function shortcutDefinition(action: ShortcutAction): ShortcutDefinition {
  const found = SHORTCUT_DEFINITIONS.find((item) => item.action === action);
  if (!found) throw new Error(`unknown shortcut action: ${action}`);
  return found;
}

export function defaultShortcutCombo(action: ShortcutAction, platform: ShortcutPlatform): ShortcutCombo {
  return shortcutDefinition(action).defaults[platform];
}

export function resolvedShortcutCombo(action: ShortcutAction, platform: ShortcutPlatform): ShortcutCombo {
  return loadCustomShortcuts()[action] ?? defaultShortcutCombo(action, platform);
}

export function loadCustomShortcuts(): Partial<Record<ShortcutAction, ShortcutCombo>> {
  if (cachedCustomShortcuts) return cachedCustomShortcuts;
  try {
    const raw = localStorage.getItem(SHORTCUTS_STORAGE_KEY);
    const parsed = raw ? JSON.parse(raw) : {};
    cachedCustomShortcuts = normalizeCustomShortcuts(parsed);
  } catch {
    cachedCustomShortcuts = {};
  }
  return cachedCustomShortcuts;
}

export function saveCustomShortcut(action: ShortcutAction, combo: ShortcutCombo | null): void {
  const next = { ...loadCustomShortcuts() };
  if (combo) {
    next[action] = normalizeCombo(combo);
  } else {
    delete next[action];
  }
  try {
    localStorage.setItem(SHORTCUTS_STORAGE_KEY, JSON.stringify(next));
  } catch {
    // Keep runtime behavior usable even when storage is unavailable.
  }
  cachedCustomShortcuts = next;
  notifyShortcutsChanged();
}

export function resetCustomShortcuts(): void {
  try {
    localStorage.removeItem(SHORTCUTS_STORAGE_KEY);
  } catch {
    // Ignore storage failures; the in-memory cache is still reset below.
  }
  cachedCustomShortcuts = {};
  notifyShortcutsChanged();
}

export function notifyShortcutsChanged(): void {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new CustomEvent(SHORTCUTS_CHANGED_EVENT));
}

export function onShortcutsChanged(callback: () => void): () => void {
  if (typeof window === "undefined") return () => {};
  const onStorage = (event: StorageEvent) => {
    if (event.key !== SHORTCUTS_STORAGE_KEY) return;
    cachedCustomShortcuts = null;
    callback();
  };
  const onCustom = () => {
    cachedCustomShortcuts = null;
    callback();
  };
  window.addEventListener("storage", onStorage);
  window.addEventListener(SHORTCUTS_CHANGED_EVENT, onCustom);
  return () => {
    window.removeEventListener("storage", onStorage);
    window.removeEventListener(SHORTCUTS_CHANGED_EVENT, onCustom);
  };
}

export function formatShortcutCombo(combo: ShortcutCombo, platform: ShortcutPlatform): string {
  return formatShortcutComboParts(combo, platform).join(platform === "darwin" ? "" : "+");
}

export function formatShortcutComboParts(combo: ShortcutCombo, platform: ShortcutPlatform): string[] {
  const normalized = normalizeCombo(combo);
  const parts: string[] = [];
  if (platform === "darwin") {
    if (normalized.meta) parts.push("⌘");
    if (normalized.ctrl) parts.push("⌃");
    if (normalized.alt) parts.push("⌥");
    if (normalized.shift) parts.push("⇧");
    parts.push(displayKey(normalized.key));
    return parts;
  }
  if (normalized.ctrl) parts.push("Ctrl");
  if (normalized.meta) parts.push("Meta");
  if (normalized.alt) parts.push("Alt");
  if (normalized.shift) parts.push("Shift");
  parts.push(displayKey(normalized.key));
  return parts;
}

export function comboFromKeyboardEvent(event: KeyboardShortcutEvent): ShortcutCombo | null {
  if (isModifierKey(event.key)) return null;
  return normalizeCombo({
    key: event.key,
    ctrl: event.ctrlKey ?? false,
    meta: event.metaKey ?? false,
    alt: event.altKey ?? false,
    shift: event.shiftKey ?? false,
  });
}

export function matchesShortcut(event: KeyboardShortcutEvent, action: ShortcutAction, platform: ShortcutPlatform): boolean {
  const combo = comboFromKeyboardEvent(event);
  if (!combo) return false;
  const definition = shortcutDefinition(action);
  if (sameCombo(combo, resolvedShortcutCombo(action, platform))) return true;
  if (loadCustomShortcuts()[action]) return false;
  return definition.aliases?.[platform]?.some((alias) => sameCombo(combo, alias)) ?? false;
}

export function shortcutConflict(
  action: ShortcutAction,
  combo: ShortcutCombo,
  platform: ShortcutPlatform,
): ShortcutDefinition | null {
  return SHORTCUT_DEFINITIONS.find((definition) => {
    if (definition.action === action) return false;
    return sameCombo(resolvedShortcutCombo(definition.action, platform), combo);
  }) ?? null;
}

export function shortcutAcceptsCombo(action: ShortcutAction, combo: ShortcutCombo): boolean {
  const allowedKeys = shortcutDefinition(action).allowedKeys;
  if (!allowedKeys || allowedKeys.length === 0) return true;
  const key = normalizeCombo(combo).key;
  return allowedKeys.some((allowedKey) => normalizeKey(allowedKey) === key);
}

export function useGlobalShortcut(
  action: ShortcutAction,
  handler: (event: globalThis.KeyboardEvent) => void,
  deps: DependencyList = [],
  enabled = true,
): void {
  const definition = shortcutDefinition(action);
  useEffect(() => {
    if (!enabled) return;
    const platform = detectShortcutPlatform();
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (isShortcutRecorderTarget(event.target)) return;
      if (!definition.allowInEditable && isEditableTarget(event.target)) return;
      if (!matchesShortcut(event, action, platform)) return;
      if (definition.preventDefault !== false) event.preventDefault();
      handler(event);
    };
    document.addEventListener("keydown", onKey, { capture: true });
    return () => document.removeEventListener("keydown", onKey, { capture: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [action, enabled, handler, ...deps]);
}

// useShortcutComboLabel resolves an action's current combo as display text
// (e.g. "Enter", "⌃Enter") and re-renders when the user rebinds shortcuts, so
// tooltips and hints never show a stale key.
export function useShortcutComboLabel(action: ShortcutAction): string {
  const [, setRevision] = useState(0);
  useEffect(() => onShortcutsChanged(() => setRevision((value) => value + 1)), []);
  const platform = detectShortcutPlatform();
  return formatShortcutCombo(resolvedShortcutCombo(action, platform), platform);
}

export function isCloseTabShortcut(event: KeyboardShortcutEvent, platform: ShortcutPlatform): boolean {
  return matchesShortcut(event, "tab.close", platform);
}

function normalizeCustomShortcuts(value: unknown): Partial<Record<ShortcutAction, ShortcutCombo>> {
  if (!value || typeof value !== "object") return {};
  const out: Partial<Record<ShortcutAction, ShortcutCombo>> = {};
  for (const definition of SHORTCUT_DEFINITIONS) {
    const raw = (value as Record<string, unknown>)[definition.action];
    if (!raw || typeof raw !== "object") continue;
    const combo = normalizeCombo(raw as ShortcutCombo);
    if (combo.key) out[definition.action] = combo;
  }
  return out;
}

function normalizeCombo(combo: ShortcutCombo): ShortcutCombo {
  const key = normalizeKey(combo.key);
  return {
    key,
    ctrl: Boolean(combo.ctrl),
    meta: Boolean(combo.meta),
    alt: Boolean(combo.alt),
    shift: Boolean(combo.shift),
  };
}

function normalizeKey(key: string): string {
  if (key === " ") return "Space";
  if (key.length === 1) return key.toLowerCase();
  return key;
}

function displayKey(key: string): string {
  if (key === " ") return "Space";
  if (key === "ArrowUp") return "↑";
  if (key === "ArrowDown") return "↓";
  if (key === "ArrowLeft") return "←";
  if (key === "ArrowRight") return "→";
  if (key.length === 1) return key.toUpperCase();
  return key;
}

function sameCombo(a: ShortcutCombo, b: ShortcutCombo): boolean {
  const left = normalizeCombo(a);
  const right = normalizeCombo(b);
  return (
    left.key === right.key &&
    Boolean(left.ctrl) === Boolean(right.ctrl) &&
    Boolean(left.meta) === Boolean(right.meta) &&
    Boolean(left.alt) === Boolean(right.alt) &&
    Boolean(left.shift) === Boolean(right.shift)
  );
}

function isModifierKey(key: string): boolean {
  return key === "Meta" || key === "Control" || key === "Alt" || key === "Shift";
}

export function isEditableTarget(target: EventTarget | null): boolean {
  if (typeof HTMLElement === "undefined") return false;
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  const tag = target.tagName.toLowerCase();
  return tag === "input" || tag === "textarea" || tag === "select";
}

export function isShortcutRecorderTarget(target: EventTarget | null): boolean {
  if (typeof HTMLElement === "undefined") return false;
  return target instanceof HTMLElement && Boolean(target.closest(".shortcuts-settings__key--recording"));
}
