// Run: tsx src/__tests__/terminal-theme.test.ts

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { JSDOM } from "jsdom";

const dom = new JSDOM("<!doctype html><html><head></head><body><div id='terminal'></div></body></html>");
Object.assign(globalThis, {
  window: dom.window,
  document: dom.window.document,
  MutationObserver: dom.window.MutationObserver,
  getComputedStyle: dom.window.getComputedStyle.bind(dom.window),
});
Object.defineProperty(dom.window, "matchMedia", {
  configurable: true,
  value: () => ({
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  }),
});

const { applyTheme } = await import("../lib/theme");
const {
  applyTerminalThemePreference,
  createTerminalThemeSaveQueue,
  getResolvedTerminalTheme,
  normalizeTerminalThemePreference,
  onTerminalThemePreferenceChange,
  terminalThemeForElement,
} = await import("../lib/terminalTheme");

assert.equal(normalizeTerminalThemePreference("unknown"), "auto");
assert.equal(normalizeTerminalThemePreference(undefined), "auto", "older settings payloads fall back safely");
assert.equal(normalizeTerminalThemePreference("light"), "light");

applyTheme("light", "graphite");
applyTerminalThemePreference("auto");
assert.equal(getResolvedTerminalTheme(), "light", "follow-app resolves the current app theme");
assert.equal(document.documentElement.hasAttribute("data-terminal-theme"), false);

applyTerminalThemePreference("dark");
assert.equal(getResolvedTerminalTheme(), "dark", "explicit terminal theme overrides the app theme");
assert.equal(document.documentElement.getAttribute("data-terminal-theme"), "dark");

let notifications = 0;
const unsubscribe = onTerminalThemePreferenceChange(() => { notifications += 1; });
applyTerminalThemePreference("light");
unsubscribe();
assert.equal(notifications, 1, "open terminals are notified when the preference changes");

const host = document.getElementById("terminal")!;
host.style.setProperty("--terminal-bg", "#fafafa");
host.style.setProperty("--terminal-fg", "#202124");
host.style.setProperty("--terminal-cursor", "#9a4f00");
const xtermTheme = terminalThemeForElement(host);
assert.equal(xtermTheme.background, "#fafafa");
assert.equal(xtermTheme.foreground, "#202124");
assert.equal(xtermTheme.cursor, "#9a4f00");
assert.equal(xtermTheme.black, "#25272a", "light terminal uses a contrast-safe ANSI palette");

function relativeLuminance(hex: string): number {
  assert.match(hex, /^#[0-9a-f]{6}$/i);
  const channels = [1, 3, 5].map((start) => Number.parseInt(hex.slice(start, start + 2), 16) / 255);
  const linear = channels.map((channel) => channel <= 0.04045
    ? channel / 12.92
    : ((channel + 0.055) / 1.055) ** 2.4);
  return 0.2126 * linear[0] + 0.7152 * linear[1] + 0.0722 * linear[2];
}

function contrastRatio(foreground: string, background: string): number {
  const [lighter, darker] = [relativeLuminance(foreground), relativeLuminance(background)]
    .sort((a, b) => b - a);
  return (lighter + 0.05) / (darker + 0.05);
}

const ansiKeys = [
  "black", "red", "green", "yellow", "blue", "magenta", "cyan", "white",
  "brightBlack", "brightRed", "brightGreen", "brightYellow", "brightBlue",
  "brightMagenta", "brightCyan", "brightWhite",
] as const;
for (const key of ansiKeys) {
  const color = xtermTheme[key];
  assert.equal(typeof color, "string");
  assert.ok(
    contrastRatio(color!, xtermTheme.background!) >= 4.5,
    `${key} must remain readable against the light terminal background`,
  );
}

applyTerminalThemePreference("dark");
const darkTerminalTheme = terminalThemeForElement(document.createElement("div"));
assert.equal(darkTerminalTheme.selectionForeground, "#ffffff");
assert.ok(
  contrastRatio(darkTerminalTheme.selectionForeground!, darkTerminalTheme.selectionBackground!) >= 4.5,
  "dark terminal selection text meets WCAG AA contrast",
);

let releaseFirstSave!: () => void;
const firstSaveGate = new Promise<void>((resolve) => { releaseFirstSave = resolve; });
const saveOrder: string[] = [];
const saveTerminalTheme = createTerminalThemeSaveQueue(async (theme) => {
  saveOrder.push(`start:${theme}`);
  if (theme === "light") await firstSaveGate;
  saveOrder.push(`end:${theme}`);
});
const firstSave = saveTerminalTheme("light");
await Promise.resolve();
const secondSave = saveTerminalTheme("dark");
await Promise.resolve();
assert.deepEqual(saveOrder, ["start:light"], "newer intent waits for the active Wails save");
releaseFirstSave();
await Promise.all([firstSave, secondSave]);
assert.deepEqual(saveOrder, ["start:light", "end:light", "start:dark", "end:dark"]);

const recoveryOrder: string[] = [];
const recoverAfterFailure = createTerminalThemeSaveQueue(async (theme) => {
  recoveryOrder.push(theme);
  if (theme === "light") throw new Error("simulated save failure");
});
const failedSave = recoverAfterFailure("light");
const recoveredSave = recoverAfterFailure("dark");
await assert.rejects(failedSave, /simulated save failure/);
await recoveredSave;
assert.deepEqual(recoveryOrder, ["light", "dark"], "a failed save does not poison newer intent");

const testDir = dirname(fileURLToPath(import.meta.url));
const terminalSource = readFileSync(resolve(testDir, "../components/TerminalView.tsx"), "utf8");
const settingsSource = readFileSync(resolve(testDir, "../components/SettingsPanel.tsx"), "utf8");
const stylesSource = readFileSync(resolve(testDir, "../styles.css"), "utf8");

function cssRuleDeclarations(css: string, selector: string): Record<string, string> {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = css.match(new RegExp(`${escapedSelector}\\s*\\{([^}]*)\\}`));
  assert.ok(match, `missing CSS rule for ${selector}`);
  return Object.fromEntries(
    match[1]
      .split(";")
      .map((declaration) => declaration.trim())
      .filter(Boolean)
      .map((declaration) => {
        const colon = declaration.indexOf(":");
        assert.ok(colon > 0, `invalid declaration in ${selector}: ${declaration}`);
        return [declaration.slice(0, colon).trim(), declaration.slice(colon + 1).trim()];
      }),
  );
}

function assertCssRule(
  css: string,
  selector: string,
  expected: Record<string, string>,
): void {
  const actual = cssRuleDeclarations(css, selector);
  for (const [property, value] of Object.entries(expected)) {
    assert.equal(actual[property], value, `${selector} must set ${property} from terminal-owned tokens`);
  }
}

const terminalStylesStart = stylesSource.indexOf("/* Integrated terminal */");
const terminalStylesEnd = stylesSource.indexOf("@media (max-width: 760px)", terminalStylesStart);
assert.ok(terminalStylesStart >= 0 && terminalStylesEnd > terminalStylesStart, "integrated terminal CSS section must exist");
const terminalStyles = stylesSource.slice(terminalStylesStart, terminalStylesEnd);

assert.ok(terminalSource.includes("terminal.options.theme = terminalThemeForElement(host)"));
assert.ok(!terminalSource.includes('background: "#111315"'));
assert.ok(settingsSource.includes("createTerminalThemeSaveQueue"));
assert.ok(settingsSource.includes("if (!terminalThemeSavePending.current)"));
assert.ok(settingsSource.includes("onTerminalTheme={setTerminalThemePreference}"));
assert.ok(stylesSource.includes(':root[data-terminal-theme="light"]'));
assert.ok(stylesSource.includes(':root[data-terminal-theme="dark"]'));
assert.doesNotMatch(terminalStyles, /var\(--(?:bg-elev-1|fg-muted)\)/, "auto mode must use defined app tokens");

assertCssRule(terminalStyles, ":root", {
  "--terminal-surface": "var(--bg-elev)",
  "--terminal-muted": "var(--fg-dim)",
  "--terminal-cursor": "var(--accent)",
});
assertCssRule(terminalStyles, ':root[data-terminal-theme="dark"] .terminal-panel', { "color-scheme": "dark" });
assertCssRule(terminalStyles, ':root[data-terminal-theme="light"] .terminal-panel', { "color-scheme": "light" });
const explicitDarkTokens = cssRuleDeclarations(stylesSource, ':root[data-terminal-theme="dark"]');
assert.ok(
  contrastRatio(explicitDarkTokens["--terminal-selection-fg"], explicitDarkTokens["--terminal-selection"]) >= 4.5,
  "explicit dark selection CSS tokens meet WCAG AA contrast",
);

const terminalChromeMatrix: Array<[string, Record<string, string>]> = [
  [".terminal-panel__header", {
    "border-bottom": "1px solid var(--terminal-border)",
    background: "var(--terminal-surface)",
  }],
  [".terminal-panel__identity span", { color: "var(--terminal-muted)" }],
  [".terminal-shell-select", {
    border: "1px solid var(--terminal-border)",
    background: "var(--terminal-active)",
    color: "var(--terminal-fg)",
  }],
  [".terminal-shell-select:hover, .terminal-shell-select:focus-visible", {
    "border-color": "var(--terminal-cursor)",
  }],
  [".terminal-icon-button", { color: "var(--terminal-muted)" }],
  [".terminal-icon-button:hover, .terminal-icon-button:focus-visible", {
    "border-color": "var(--terminal-border)",
    background: "var(--terminal-active)",
    color: "var(--terminal-fg)",
  }],
  [".terminal-session--active", {
    "border-bottom-color": "var(--terminal-cursor)",
    background: "var(--terminal-active)",
  }],
  [".terminal-empty__spinner", { "border-top-color": "var(--terminal-cursor)" }],
];
for (const [selector, expected] of terminalChromeMatrix) {
  assertCssRule(terminalStyles, selector, expected);
}
assertCssRule(stylesSource, ".layout--terminal-drawer-expanded .terminal-drawer", {
  background: "var(--terminal-bg, #111315)",
  "border-top": "1px solid var(--terminal-border)",
});
assertCssRule(stylesSource, ".layout--terminal-drawer-open .terminal-drawer .terminal-panel__header", {
  "border-bottom": "1px solid var(--terminal-border)",
});

console.log("terminal theme tests passed");
