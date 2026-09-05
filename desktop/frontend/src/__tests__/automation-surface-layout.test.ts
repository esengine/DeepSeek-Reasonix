import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { JSDOM } from "jsdom";

const read = (path: string) => readFileSync(new URL(path, import.meta.url), "utf8");
const app = read("../App.tsx");
const isolation = read("../lib/useManagementWorkspace.ts");
const shell = read("../components/ManagementPageShell.tsx");
const css = read("../components/ManagementPageShell.css");
const heartbeat = read("../custom/features/heartbeat/HeartbeatPanel.tsx");
const warmth = read("../lib/useWarmTerminalPanel.ts");

// The shared full-window shell replaces the old chat-pane projection. Background
// geometry and component identity survive while all workspace input is inert.
assert.match(app, /useManagementWorkspace\(layoutRef, managementActive\)/);
assert.match(isolation, /workspace\.inert = true/);
assert.match(isolation, /workspace\.inert = false/);
assert.doesNotMatch(app, /mainView === "automation"/);
assert.match(app, /inert=\{managementActive\}/);
assert.match(css, /\.management-screen \{[^}]*position: fixed;[^}]*inset: 0;/);
assert.match(shell, /hidden=\{!active\} inert=\{!active\}/);
assert.match(app, /if \(managementActive\) returnToWorkspace\(\)/);
assert.match(heartbeat, /<ManagementPageShell active=\{active\}/);
assert.match(css, /management-titlebar-height: 48px/);
assert.match(app, /useWarmTerminalPanel\(terminalPanelOpen, terminalResizing, !managementActive\)/);
assert.match(warmth, /if \(open\) setMounted\(true\)/);
assert.match(warmth, /if \(!open \|\| !visible\) \{\s*setFitEnabled\(false\)/s);

// Exercise the selector actually used by App against the header actually emitted
// by the shared shell. A class rename must not silently break native double-click.
const selector = app.match(/const onChromeSurface = target\?\.closest\("([^"]+)"\)/)![1];
const headerClass = shell.match(/<header className="([^"]+)"/)![1];
const dom = new JSDOM(`<section><header class="${headerClass}"></header><main><button>Back</button></main></section>`);
assert(dom.window.document.querySelector("header")!.closest(selector));
assert.equal(dom.window.document.querySelector("button")!.closest(selector), null);
assert.match(app, /desktopPlatform === "darwin"/);
assert.match(app, /windowsFramelessChrome \|\| desktopPlatform/);
assert.match(app, /target\?\.closest\("button, input, textarea, select, a,/);
dom.window.close();
console.log("PASS shared management geometry, input isolation, terminal retention and native titlebar dispatch");
