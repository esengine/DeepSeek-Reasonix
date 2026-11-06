import type { ITheme } from "@xterm/xterm";

// The terminal is drawn in the page's own colours, read where it sits so a
// theme change is one read away rather than a second palette to keep.
export function terminalTheme(from: HTMLElement): ITheme {
  const css = getComputedStyle(from);
  const get = (name: string, fallback: string) => css.getPropertyValue(name).trim() || fallback;
  const text = get("--text", "#292b29");
  const green = get("--ok-ink", "#2f7d4f");
  const red = get("--err-ink", "#b3402f");
  const amber = get("--warn-ink", "#8a6a1f");
  const blue = get("--net-ink", "#2f6f95");
  const muted = get("--muted", "#5e625c");
  return {
    background: get("--raised", "#ffffff"),
    foreground: text,
    cursor: text,
    cursorAccent: get("--raised", "#ffffff"),
    selectionBackground: get("--net-wash", "#e4e8f4"),
    selectionInactiveBackground: get("--overlay", "#e9e9e5"),
    black: text, red, green, yellow: amber, blue, magenta: blue, cyan: blue, white: muted,
    brightBlack: muted, brightRed: red, brightGreen: green, brightYellow: amber, brightBlue: blue, brightMagenta: blue, brightCyan: blue, brightWhite: text,
  };
}

export function monoFamily(from: HTMLElement): string {
  return getComputedStyle(from).getPropertyValue("--mono").trim() || "ui-monospace, Menlo, Consolas, monospace";
}
