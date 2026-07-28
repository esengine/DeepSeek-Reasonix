// Run: tsx src/__tests__/browser-annotation.test.ts

import {
  buildBrowserAnnotationText,
  cssPathForElement,
  rectFromPoints,
  toBrowserSelectedTextReference,
} from "../lib/browserAnnotation";
import {
  browserPageUrlFromPath,
  browserReferencePath,
  clearBrowserUrlForSession,
  DEFAULT_BROWSER_URL,
  isBrowserReferencePath,
  isLocalBrowserHost,
  loadBrowserUrlForSession,
  normalizeBrowserUrl,
  rememberNativeBrowserUrl,
  sameNativeBrowserUrl,
  saveBrowserUrlForSession,
} from "../lib/browserUrl";
import { formatSelectionLabel } from "../lib/selectedTextContext";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (JSON.stringify(a) === JSON.stringify(b)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

function truthy(value: unknown, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected truthy, got ${JSON.stringify(value)}\n`);
    failed += 1;
  }
}

console.log("\nbrowser url + annotation");

eq(DEFAULT_BROWSER_URL, "", "default browser URL is empty");
eq(normalizeBrowserUrl("localhost:3000"), "http://localhost:3000/", "normalizes localhost host:port");
eq(normalizeBrowserUrl("https://example.com/app"), "https://example.com/app", "keeps https URLs");
eq(normalizeBrowserUrl("ftp://example.com"), null, "rejects non-http schemes");
eq(normalizeBrowserUrl("https://baidu.com/? 1"), "https://baidu.com/?1", "strips spaces in URL");
eq(normalizeBrowserUrl(""), null, "rejects empty input");
truthy(isLocalBrowserHost("http://127.0.0.1:5173/"), "detects loopback host");
eq(isLocalBrowserHost("https://example.com"), false, "rejects public host as local");

rememberNativeBrowserUrl("");
eq(sameNativeBrowserUrl("https://example.com/"), false, "empty native cache is not same");
rememberNativeBrowserUrl("https://example.com/path");
eq(sameNativeBrowserUrl("https://example.com/path"), true, "matches remembered native URL");
eq(sameNativeBrowserUrl("https://other.example/"), false, "rejects different native URL");
rememberNativeBrowserUrl("");

// Node test runner: stub localStorage for session-scoped URL persistence.
const memoryStore = new Map<string, string>();
const localStorageStub = {
  getItem: (key: string) => memoryStore.get(key) ?? null,
  setItem: (key: string, value: string) => { memoryStore.set(key, String(value)); },
  removeItem: (key: string) => { memoryStore.delete(key); },
};
(globalThis as { window?: { localStorage: typeof localStorageStub } }).window = {
  localStorage: localStorageStub,
};

clearBrowserUrlForSession("tab-a");
clearBrowserUrlForSession("tab-b");
eq(loadBrowserUrlForSession("tab-a"), "", "new session browser URL is empty");
saveBrowserUrlForSession("tab-a", "https://www.baidu.com/");
saveBrowserUrlForSession("tab-b", "https://www.google.com/");
eq(loadBrowserUrlForSession("tab-a"), "https://www.baidu.com/", "session A keeps its browser URL");
eq(loadBrowserUrlForSession("tab-b"), "https://www.google.com/", "session B keeps a different browser URL");
clearBrowserUrlForSession("tab-a");
eq(loadBrowserUrlForSession("tab-a"), "", "cleared session browser URL is empty again");
clearBrowserUrlForSession("tab-b");
eq(loadBrowserUrlForSession(""), "", "empty session key stays empty");


const refPath = browserReferencePath("http://localhost:3000/", "button.cta");
truthy(isBrowserReferencePath(refPath), "browser reference path prefix");
eq(browserPageUrlFromPath(refPath), "http://localhost:3000/", "strips browser path and selector");

const payload = {
  url: "http://localhost:3000/",
  note: "Make this button larger",
  text: "Workspace",
  selector: "button.cta",
  tagName: "BUTTON",
  rect: { x: 10, y: 20, width: 100, height: 40 },
};
const text = buildBrowserAnnotationText(payload);
truthy(text.includes("URL: http://localhost:3000/"), "annotation includes URL");
truthy(text.includes("Requested change:\nMake this button larger"), "annotation includes note");
truthy(text.includes("Selected content:\nWorkspace"), "annotation includes selected text");

const selected = toBrowserSelectedTextReference(payload, "browser-1");
eq(selected.id, "browser-1", "reference id");
truthy(isBrowserReferencePath(selected.path), "reference uses browser path");
const label = formatSelectionLabel(selected);
truthy(label.startsWith("[Browser:"), `selection label uses Browser prefix (${label})`);

eq(rectFromPoints({ x: 5, y: 8 }, { x: 25, y: 18 }), { x: 5, y: 8, width: 20, height: 10 }, "rectFromPoints normalizes drag");

if (typeof document !== "undefined") {
  const root = document.createElement("div");
  root.id = "root";
  const child = document.createElement("span");
  root.appendChild(child);
  document.body?.appendChild(root);
  eq(cssPathForElement(child), "#root > span", "css path prefers id ancestors");
  root.remove();
} else {
  // jsdom may be unavailable in plain tsx runs; skip DOM-only assertion.
  process.stdout.write("  SKIP  css path (no document)\n");
}

console.log(`\nbrowser annotation: ${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
