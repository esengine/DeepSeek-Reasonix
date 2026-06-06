import type { ReactNode } from "react";

export type ShortcutKey = "mod" | "shift" | "enter" | "tab" | "esc" | "updown" | string;

function isMacPlatform(): boolean {
  if (typeof document === "undefined") return false;
  return document.documentElement.dataset.platform === "macos";
}

function keyLabel(key: ShortcutKey, mac: boolean): string {
  switch (key) {
    case "mod":
    case "⌘":
      return mac ? "⌘" : "Ctrl";
    case "shift":
    case "⇧":
      return mac ? "⇧" : "Shift";
    case "enter":
    case "⏎":
      return "Enter";
    case "tab":
    case "⇥":
      return "Tab";
    case "esc":
      return "Esc";
    case "updown":
    case "↑↓":
      return "↑↓";
    default:
      return key;
  }
}

export function shortcutText(keys: readonly ShortcutKey[]): string {
  const mac = isMacPlatform();
  return keys.map((key) => keyLabel(key, mac)).join(mac ? "" : "+");
}

export function localizeShortcutText(text: string): string {
  return text.replace(/⌘/g, keyLabel("mod", isMacPlatform()));
}

export function Shortcut({
  keys,
  className,
}: {
  keys: readonly ShortcutKey[];
  className?: string;
}): ReactNode {
  const mac = isMacPlatform();
  return (
    <span className={["shortcut", className].filter(Boolean).join(" ")}>
      {keys.map((key, index) => (
        <kbd key={`${key}-${index}`} data-key={key}>
          {keyLabel(key, mac)}
        </kbd>
      ))}
    </span>
  );
}
