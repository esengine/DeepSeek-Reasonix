// Run: npx tsx src/__tests__/history-load-failure-contract.test.ts
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const controller = readFileSync(join(root, "lib/useController.ts"), "utf8");
const store = readFileSync(join(root, "lib/transcriptStore.ts"), "utf8");
const app = readFileSync(join(root, "App.tsx"), "utf8");
const transcript = readFileSync(join(root, "components/Transcript.tsx"), "utf8");

assert.match(controller, /deferResetUntilHistory \?\? true/, "history reset waits for successful load");
assert.match(controller, /type: "hydrate_error"/, "history failure dispatches hydrate_error");
assert.match(controller, /applyHydrateErrorState|hydratePlaceholderItems/, "hydrate_error keeps previous content");
assert.match(readFileSync(join(root, "lib/hydrateErrorState.ts"), "utf8"), /keptItems/, "hydrateErrorState preserves items");
assert.match(controller, /throw new Error\(t\("history\.failedLoadHistory"\)\)/, "listSessions does not swallow failures as empty");
assert.match(controller, /retrySessionHistory/, "retry path is exported");
assert.match(
  transcript,
  /useLayoutEffect\(\(\) => \{\n    if \(!virtuosoReadyRef\.current \|\| !stick\.current\) return;\n    scrollToBottom\(\);\n  \}, \[footerHeight, scrollToBottom, stick\]\);/,
  "a tail-owned footer resize uses the immediate and bounded bottom transaction before paint without moving a manual reader",
);
assert.match(
  transcript,
  /if \(!hydrating \|\| scrollModeRef\.current === "tail-follow"\) followGrowingTail\("viewport-resize"\);/,
  "a hydrating transcript still repairs a restored-draft viewport resize when it owns tail-follow without moving a manual reader",
);
assert.match(controller, /shouldPreferResidentHistory\(resetSurface, options\.preserveCachedHistory\)/, "retry hydrates fetch instead of serving the resident snapshot");
assert.match(
  controller,
  /loadSessionDataForTab\(tabId, false, "startup", \{ preserveCachedHistory: true \}\)/,
  "failed clear keeps the visible transcript instead of a resident snapshot",
);
assert.match(store, /slice\.error/, "transcript store rejects slice.error as failure");
assert.match(app, /retrySessionHistory/, "App wires history retry control");
assert.match(app, /history-load-error/, "App surfaces hydrate error banner");

console.log("  PASS  history load failure contract");
