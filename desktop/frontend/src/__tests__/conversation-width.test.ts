// Run: tsx src/__tests__/conversation-width.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  applyConversationWidth,
  CONVERSATION_WIDTH_STORAGE_KEY,
  FULL_CONVERSATION_MAX_WIDTH,
  getCachedConversationWidth,
  normalizeConversationWidth,
  STANDARD_CONVERSATION_MAX_WIDTH,
} from "../lib/conversationWidth";

const testDir = dirname(fileURLToPath(import.meta.url));
const stylesSource = readFileSync(resolve(testDir, "../styles.css"), "utf8");
const settingsSource = readFileSync(resolve(testDir, "../components/SettingsPanel.tsx"), "utf8");

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

const attrs = new Map<string, string>();
const styleProps = new Map<string, string>();
const storage = new Map<string, string>();

(globalThis as unknown as { document: unknown }).document = {
  documentElement: {
    setAttribute(name: string, value: string) {
      attrs.set(name, value);
    },
    style: {
      setProperty(name: string, value: string) {
        styleProps.set(name, value);
      },
    },
  },
};

(globalThis as unknown as { localStorage: unknown }).localStorage = {
  getItem(key: string) {
    return storage.get(key) ?? null;
  },
  setItem(key: string, value: string) {
    storage.set(key, value);
  },
};

console.log("\nconversation width contract");

ok(normalizeConversationWidth(undefined) === "standard", "missing values use the backward-compatible standard width");
ok(normalizeConversationWidth("unknown") === "standard", "unknown values use the standard width");
ok(normalizeConversationWidth("full") === "full", "full is preserved");

storage.set(CONVERSATION_WIDTH_STORAGE_KEY, "full");
ok(getCachedConversationWidth() === "full", "early-paint cache restores full width");

applyConversationWidth("standard");
ok(styleProps.get("--maxw") === STANDARD_CONVERSATION_MAX_WIDTH, "standard width remains fixed at 960px");

applyConversationWidth("full");
ok(styleProps.get("--maxw") === FULL_CONVERSATION_MAX_WIDTH, "full width never becomes narrower than standard");
ok(attrs.get("data-conversation-width") === "full", "active width is exposed on the root element");
ok(storage.get(CONVERSATION_WIDTH_STORAGE_KEY) === "full", "applied width refreshes the early-paint cache");

ok(stylesSource.includes("--maxw-inner: 100%;"), "nested transcript content uses an unbounded inner width");
ok(
  /\.warm-turn__body\s*>\s*\*\s*\{[^}]*max-width:\s*var\(--maxw-inner\)/s.test(stylesSource) &&
    /\.readonly-batch__body\s*>\s*\*,\s*\.turn-collapse__body\s*>\s*\*\s*\{[^}]*max-width:\s*var\(--maxw-inner\)/s.test(stylesSource),
  "nested transcript containers do not compound the outer percentage width",
);
ok(!stylesSource.includes("--maxw: 90%;"), "stylesheets cannot reintroduce the narrow 90 percent override");

ok(
  settingsSource.includes("setConversationWidth(applyConversationWidth(s.conversationWidth))"),
  "authoritative desktop settings replace the early-paint cache",
);
ok(
  settingsSource.includes("conversationWidth: normalizeConversationWidth(view.conversationWidth)"),
  "older settings payloads without the field normalize to standard",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
