// Run: tsx src/__tests__/font-family-terminal.test.ts

import {
  MONO_FONT_CHANGED_EVENT,
  applyMonoFontFamily,
  monoFontStackForTerminal,
} from "../lib/fontFamily";

const MONO_KEY = "reasonix-mono-font-family";
const CUSTOM_MONO_KEY = "reasonix-mono-font-family-custom";

let passed = 0;
let failed = 0;

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
    failed += 1;
  }
}

const store = new Map<string, string>();
Object.defineProperty(globalThis, "localStorage", {
  configurable: true,
  value: {
    getItem: (key: string) => store.get(key) ?? null,
    setItem: (key: string, value: string) => {
      store.set(key, value);
    },
  },
});
Object.defineProperty(globalThis, "document", {
  configurable: true,
  value: {
    documentElement: {
      setAttribute: () => {},
      removeAttribute: () => {},
      style: {
        removeProperty: () => {},
        setProperty: () => {},
      },
    },
  },
});
const dispatched: string[] = [];
Object.defineProperty(globalThis, "window", {
  configurable: true,
  value: {
    dispatchEvent: (event: Event) => {
      dispatched.push(event.type);
    },
  },
});

function setMono(font: string, custom = "") {
  store.clear();
  store.set(MONO_KEY, font);
  if (custom) store.set(CUSTOM_MONO_KEY, custom);
}

console.log("\nterminal monospace font stack");

setMono("system");
eq(monoFontStackForTerminal().includes("ui-monospace"), true, "system maps to the platform monospace stack");

setMono("jetbrains");
eq(monoFontStackForTerminal().startsWith('"JetBrains Mono"'), true, "jetbrains preset resolves first");

setMono("cascadia");
eq(monoFontStackForTerminal().startsWith('"Cascadia Code"'), true, "cascadia preset resolves first");

setMono("sfmono");
eq(monoFontStackForTerminal().startsWith('"SF Mono"'), true, "sfmono preset resolves first");

setMono("custom", "Hack NF");
eq(
  monoFontStackForTerminal().startsWith('"Hack NF", "Cascadia Code"'),
  true,
  "custom nerd-font name is quoted and keeps the mono fallback",
);

setMono("custom", '"FiraCode Nerd Font Mono"');
eq(
  monoFontStackForTerminal().startsWith('"FiraCode Nerd Font Mono"'),
  true,
  "already-quoted custom names are kept as-is",
);

setMono("custom", "Menlo");
eq(monoFontStackForTerminal().startsWith("Menlo,"), true, "single-word custom names stay unquoted");

setMono("custom", "");
eq(monoFontStackForTerminal().includes("ui-monospace"), true, "empty custom name falls back to the system stack");

dispatched.length = 0;
store.clear();
applyMonoFontFamily("jetbrains");
eq(dispatched.includes(MONO_FONT_CHANGED_EVENT), true, "font change dispatches the terminal sync event");
eq(store.get(MONO_KEY), "jetbrains", "preference is still persisted");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
