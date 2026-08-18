// Run: tsx src/__tests__/navigation-surface-transition.test.ts

import { readFileSync } from "node:fs";
import { guardBackendNavigationResult, settleNavigationSurfaceIntent } from "../lib/navigationSurfaceTransition";

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  process.stdout.write(`  ${value ? "PASS" : "FAIL"}  ${label}\n`);
  if (value) passed += 1;
  else failed += 1;
}

console.log("\nnavigation surface transition");

let active: number | null = 1;
active = 2; // B supersedes A before A completes.
active = 3; // C supersedes queued B.
active = settleNavigationSurfaceIntent(active, 1);
ok(active === 3, "A completion cannot release C's mask");
active = settleNavigationSurfaceIntent(active, 2);
ok(active === 3, "coalesced B completion cannot release C's mask");
active = settleNavigationSurfaceIntent(active, 3);
ok(active === null, "the latest completion releases its own mask");

let reasserted = "";
const currentAccepted = await guardBackendNavigationResult({
  intent: 4,
  targetTabId: "tab-current",
  kind: "tab.reveal-background",
  isIntentCurrent: (intent) => intent === 4,
  reassert: async (kind, tabId) => { reasserted = `${kind}:${tabId}`; },
});
ok(currentAccepted, "the current backend navigation result is accepted");
ok(reasserted === "", "the current backend navigation result does not reassert");

let releaseReassert!: () => void;
const reassertGate = new Promise<void>((resolve) => { releaseReassert = resolve; });
let staleReassertStarted = false;
const staleAcceptedPromise = guardBackendNavigationResult({
  intent: 4,
  targetTabId: "tab-stale",
  kind: "tab.reveal-background",
  isIntentCurrent: (intent) => intent === 5,
  reassert: async (kind, tabId) => {
    staleReassertStarted = true;
    reasserted = `${kind}:${tabId}`;
    await reassertGate;
  },
});
await Promise.resolve();
ok(staleReassertStarted, "a stale backend-activating result starts visible-tab reassertion");
releaseReassert();
ok(await staleAcceptedPromise === false, "a stale backend-activating result is rejected after reassertion");
ok(reasserted === "tab.reveal-background:tab-stale", "stale reassertion receives the mutating target identity");

const appSource = readFileSync(new URL("../App.tsx", import.meta.url), "utf8");
ok(appSource.includes("flushSync(() => setNavigationSurfaceIntent(intent))"), "navigation masking commits synchronously before the Wails await");
ok(appSource.includes("items={runtimeTransitioning ? [] : displayItems}"), "App removes source transcript rows during navigation");
ok(appSource.includes("live={runtimeTransitioning ? undefined : state.live}"), "App removes source live output during navigation");
ok(appSource.includes("hidden={composerSurfaceHidden || undefined}"), "App keeps the composer mounted but hidden during navigation");
ok(appSource.includes("inert={composerSurfaceHidden ? true : undefined}"), "the hidden composer is inert during navigation");
ok(appSource.includes("!runtimeTransitioning && showTodos"), "source-session Todo content is isolated");
ok(appSource.includes("!runtimeTransitioning && rewindState"), "source-session rewind content is isolated");
ok((appSource.match(/guardBackendNavigationResult\(\{/g) ?? []).length === 2, "both Reveal paths guard stale backend activation results");
const switchFolderSource = appSource.slice(
  appSource.indexOf("const switchFolder = useCallback"),
  appSource.indexOf("const refreshProjectsAndTabs = useCallback"),
);
ok(switchFolderSource.includes("const navigationIntentSeq = noteNavigationIntent()"), "workspace navigation claims the shared intent before Wails");
ok(switchFolderSource.includes("beginNavigationSurface(navigationIntentSeq)"), "workspace navigation masks the source surface before Wails");
ok(switchFolderSource.includes("pickWorkspace(navigationIntentSeq)"), "folder-pick navigation carries the shared intent into the controller");
ok(switchFolderSource.includes("switchWorkspace(path, navigationIntentSeq)"), "direct workspace navigation carries the shared intent into the controller");
ok(switchFolderSource.includes("settleNavigationSurface(navigationIntentSeq)"), "workspace navigation compare-clears its surface mask");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
