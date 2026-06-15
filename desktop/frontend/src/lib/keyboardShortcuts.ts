export type ShortcutPlatform = "darwin" | "windows" | "linux";

type KeyboardShortcutEvent = Pick<globalThis.KeyboardEvent, "key" | "ctrlKey" | "metaKey">;

export function isCloseTabShortcut(event: KeyboardShortcutEvent, platform: ShortcutPlatform): boolean {
  if (event.key.toLowerCase() !== "w") return false;
  return platform === "darwin" ? event.metaKey : event.ctrlKey;
}
