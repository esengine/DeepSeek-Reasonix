// Run: tsx src/__tests__/btw-dock-layout.test.ts

import { readFileSync } from "node:fs";

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

const app = readFileSync(new URL("../App.tsx", import.meta.url), "utf8");
const styles = readFileSync(new URL("../styles.css", import.meta.url), "utf8");
const layoutStore = readFileSync(new URL("../store/layout.ts", import.meta.url), "utf8");

console.log("\nBTW dock layout");

ok(
  layoutStore.includes('export type RightDockMode = "context" | "files" | "changed" | "btw"'),
  "BTW is a first-class right-dock mode",
);
ok(
  app.includes('className={`workbench-dock__tab${rightDockMode === "btw"')
    && app.includes('onClick={() => openBtwFromCommand("")}'),
  "the workbench exposes a BTW tab",
);
ok(
  app.lastIndexOf("<BtwPanel") > app.indexOf('<div className="workbench-dock__body">'),
  "BTW renders inside the shared workbench dock body",
);
ok(!app.includes("main--with-btw") && !styles.includes(".main--with-btw"), "BTW no longer splits only the main transcript");
ok(
  app.includes('hidden={!workspacePanelRenderable}')
    && app.includes('visible={workspacePanelRenderable && rightDockMode === "btw" && btwOpen}'),
  "the dock may hide while the session-local BTW view stays mounted",
);
ok(
  styles.includes(".layout--btw-focused .chat-pane")
    && styles.includes(".layout--btw-focused .workbench-dock"),
  "narrow layouts keep BTW accessible as a focused dock page",
);
ok(
  !app.includes("previousSurfaceWasOpen")
    && app.includes("for (const [tabId, nextKey] of Object.entries(btwSessionKeyByTabId))")
    && app.includes("void app.ReturnFromBtwForTab(tabId).catch(() => undefined)")
    && app.includes("sessionKeyInvalidations={btwSessionKeyInvalidations}"),
  "session changes release the previous BTW runtime for active and background tabs",
);

if (failed > 0) {
  process.stderr.write(`\n${failed} failed, ${passed} passed\n`);
  process.exit(1);
}
process.stdout.write(`\n${passed} passed\n`);
