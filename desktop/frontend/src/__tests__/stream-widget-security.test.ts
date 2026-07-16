// Run: tsx src/__tests__/stream-widget-security.test.ts
//
// Locks the StreamWidget sandbox contract so the iframe cannot escape to the
// Wails host and the prompt/link bridges stay behind host confirmation.
// Regression cover for the #5749 review findings F1 (opaque origin + no
// trusted-payload gesture), F4 (openExternal + CSP hardening) and the theme
// channel that keeps interactive state across theme switches.

import { buildWidgetDoc, routeWidgetMessage, type WidgetMessageHandlers } from "../components/StreamWidget";
import { readFileSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));

let passed = 0;
let failed = 0;

function ok(value: unknown, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

// --- F1: opaque-origin document ---------------------------------------------

const doc = buildWidgetDoc("<div class=\"card\">hi</div>", ":root{--fg:#000}");

ok(!/allow-same-origin/.test(doc), "document never opts into allow-same-origin");
ok(doc.includes(":root{--fg:#000}"), "theme is inlined into the document for first paint");
ok(doc.includes("<div class=\"card\">hi</div>"), "widget body is embedded");
// F3: the widget applies host-pushed theme updates instead of being rebuilt.
ok(/d\.type==="theme"/.test(doc), "bootstrap applies theme updates pushed by the host");

// --- F4: CSP hardening ------------------------------------------------------

const csp = (doc.match(/Content-Security-Policy" content="([^"]+)"/) || [])[1] || "";
ok(csp.includes("default-src 'none'"), "CSP default-src is 'none'");
ok(csp.includes("img-src data:") && !/img-src[^;]*https:/.test(csp), "img-src no longer allows arbitrary https beacons");
ok(csp.includes("object-src 'none'"), "CSP locks object-src");
ok(csp.includes("base-uri 'none'"), "CSP locks base-uri");
ok(csp.includes("connect-src 'none'"), "CSP keeps connect-src 'none'");
ok(/script-src[^;]*cdnjs\.cloudflare\.com/.test(csp), "CSP keeps the CDN script allowlist");

// --- routing gates ----------------------------------------------------------

function collect() {
  const calls: { setHeight: number[]; requestPrompt: string[]; requestLink: string[] } = {
    setHeight: [], requestPrompt: [], requestLink: [],
  };
  const handlers: WidgetMessageHandlers = {
    setHeight: (px) => calls.setHeight.push(px),
    requestPrompt: (t) => calls.requestPrompt.push(t),
    requestLink: (u) => calls.requestLink.push(u),
  };
  return { calls, handlers };
}

// height clamps
{
  const { calls, handlers } = collect();
  routeWidgetMessage({ type: "ws", height: 5 }, handlers);
  routeWidgetMessage({ type: "ws", height: 99999 }, handlers);
  routeWidgetMessage({ type: "ws", height: 320 }, handlers);
  ok(calls.setHeight.join(",") === "50,2000,320", "height is clamped to [50, 2000]");
}

// F1: a widget message only *requests* an action — it never sends directly, and
// the payload carries no trusted flag that could bypass host confirmation.
{
  const { calls, handlers } = collect();
  routeWidgetMessage({ type: "wp", text: "run this" }, handlers);
  // Even a payload that forges a would-be gesture flag still only requests.
  routeWidgetMessage({ type: "wp", text: "forged", gesture: true }, handlers);
  ok(calls.requestPrompt.join("|") === "run this|forged", "prompt messages surface a confirmation request, never an auto-send");
  routeWidgetMessage({ type: "wp", text: 123 as unknown as string }, handlers);
  ok(calls.requestPrompt.length === 2, "non-string prompt text is ignored");
}

// F4/F1: link protocol allowlist, and links also require confirmation.
{
  const { calls, handlers } = collect();
  routeWidgetMessage({ type: "wl", url: "https://example.com" }, handlers);
  routeWidgetMessage({ type: "wl", url: "http://example.com" }, handlers);
  routeWidgetMessage({ type: "wl", url: "mailto:a@b.com" }, handlers);
  routeWidgetMessage({ type: "wl", url: "javascript:alert(1)" }, handlers);
  routeWidgetMessage({ type: "wl", url: "file:///etc/passwd" }, handlers);
  ok(
    calls.requestLink.join("|") === "https://example.com|http://example.com|mailto:a@b.com",
    "only http(s)/mailto links can be offered; they still require confirmation",
  );
}

// junk payloads are ignored
{
  const { calls, handlers } = collect();
  routeWidgetMessage(null, handlers);
  routeWidgetMessage("string", handlers);
  routeWidgetMessage({ type: "unknown" }, handlers);
  ok(
    calls.setHeight.length === 0 && calls.requestPrompt.length === 0 && calls.requestLink.length === 0,
    "malformed messages are ignored",
  );
}

// --- Contract: SHELL_CSS classes match what widget-readme.md documents ---------

// The widget README tells models these CSS classes are pre-loaded. If they are
// ever removed from SHELL_CSS, this test catches the mismatch so the docs can
// be updated in lockstep.
const shellSrc = readFileSync(
  resolve(__dirname, "../components/StreamWidget.tsx"),
  "utf8",
);

const EXPECTED_CLASSES = [
  ".card", ".badge", ".badge.primary",
  ".metric", ".metric-value", ".metric-label", ".label",
  ".row", ".col", ".grid-2", ".grid-3",
  ".t", ".ts", ".th", ".box", ".node", ".arr", ".leader",
  ".c-blue", ".c-teal", ".c-amber", ".c-green", ".c-red",
  ".c-purple", ".c-coral", ".c-pink", ".c-gray",
  "button.primary", "button.destructive",
];

function escapeRegExp(s: string): string {
  // Escape special regex chars so they are matched literally.
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

for (const cls of EXPECTED_CLASSES) {
  ok(new RegExp(escapeRegExp(cls)).test(shellSrc), `SHELL_CSS contains ${cls}`);
}

// shell is built with SHELL_CSS embedded
ok(doc.includes(`<div class="card">hi</div>`), "widget body appears in built doc");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
