// Run: npx tsx src/__tests__/history-load-failure-contract.test.ts
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { fetchPreparedHistorySlice } from "../lib/transcriptHistoryFetch";
import type { HistorySlice } from "../lib/types";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const controller = readFileSync(join(root, "lib/useController.ts"), "utf8");
const store = readFileSync(join(root, "lib/transcriptStore.ts"), "utf8");
const chatPane = readFileSync(join(root, "app-shell/ChatPaneRegion.tsx"), "utf8");
const appView = readFileSync(join(root, "app-shell/AppRuntimeView.tsx"), "utf8");

assert.match(controller, /deferResetUntilHistory \?\? true/, "history reset waits for successful load");
assert.match(controller, /type: "hydrate_error"/, "history failure dispatches hydrate_error");
assert.match(controller, /applyHydrateErrorState|hydratePlaceholderItems/, "hydrate_error keeps previous content");
assert.match(readFileSync(join(root, "lib/hydrateErrorState.ts"), "utf8"), /keptItems/, "hydrateErrorState preserves items");
assert.match(controller, /throw new Error\(t\("history\.failedLoadHistory"\)\)/, "listSessions does not swallow failures as empty");
assert.match(controller, /retrySessionHistory/, "retry path is exported");
assert.match(controller, /startTranscriptFollow/, "retry establishes an authoritative Follow snapshot");
assert.match(
  controller,
  /loadSessionDataForTab\(tabId, false, "startup", \{ preserveCachedHistory: true \}\)/,
  "failed clear keeps the visible transcript instead of a resident snapshot",
);
assert.match(store, /fetchPreparedHistorySlice/, "transcript store uses the shared history reader");
await assert.rejects(
  fetchPreparedHistorySlice(async () => ({ entries: [], error: " history unavailable " }) as unknown as HistorySlice, () => true),
  { message: "history unavailable" },
  "history reader rejects slice errors instead of treating them as empty history",
);
assert.match(appView, /retrySessionHistory/, "App wires history retry control");
assert.match(chatPane, /SessionRecoveryBanner/, "App surfaces persistent history recovery above the transcript");

console.log("  PASS  history load failure contract");
