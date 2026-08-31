import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const appSource = readFileSync(resolve(here, "../App.tsx"), "utf8");
const projectTreeSubscription = appSource.slice(
  appSource.indexOf("const stopProjectTree = onProjectTreeChanged"),
  appSource.indexOf("const stopProjectTree = onProjectTreeChanged") + 500,
);

assert.match(
  projectTreeSubscription,
  /void refreshHistoryView\(\)/,
  "project-tree changes refresh an open HistoryPanel session snapshot",
);
assert.ok(
  appSource.indexOf("const refreshHistoryView = useCallback") < appSource.indexOf("const stopProjectTree = onProjectTreeChanged"),
  "the history refresh callback is initialized before the project-tree subscription uses it",
);

console.log("history refresh contract passed");
