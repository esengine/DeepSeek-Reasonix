// Run: tsx src/__tests__/history-catalog-body-hits.test.ts
//
// Body-only history search must remain visible when metadata session matches
// are empty, and load-more must advance the search cursor independently.

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const panel = readFileSync(join(root, "components/HistoryPanel.tsx"), "utf8");
const hook = readFileSync(join(root, "lib/useHistoryCatalog.ts"), "utf8");

assert.match(panel, /hasBodyHits/, "HistoryPanel must branch on body hits");
assert.match(panel, /searchHits\.length > 0/, "HistoryPanel must keep body hits out of empty state");
assert.match(panel, /!isTrash && \(\s*<label className="mem-search history-search"/, "history search input stays available without metadata sessions");
assert.match(panel, /isTrash && sessions\.length > 0/, "trash search may still gate on session rows");
assert.match(hook, /nextSearchCursor/, "useHistoryCatalog must track an independent body cursor");
assert.match(hook, /append[\s\S]*bodyPage\.items/, "useHistoryCatalog must append body hits on load more");
assert.match(hook, /!append \|\| searchCursor/, "load-more must not restart body search with an empty cursor");

console.log("  PASS  history body-only hits contract");
