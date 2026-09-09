// Run: tsx src/__tests__/navigation-surface-transition.test.ts

import { readFileSync } from "node:fs";
import { navigateWorkspace } from "../app-runtime/navigationOwner";
import {
  advanceSurfacePaintCommit,
  beginNavigationSurfaceState,
  createNavigationSurfaceTicket,
  guardBackendNavigationResult,
  markNavigationTargetMasked,
  matchesNavigationSurfaceTicket,
  settleNavigationSurfaceIntent,
  settleNavigationSurfaceState,
} from "../lib/navigationSurfaceTransition";

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

let surface = beginNavigationSurfaceState(9);
surface = markNavigationTargetMasked(surface, 8);
ok(surface?.phase === "source-retained", "a stale request cannot replace the retained source");
surface = markNavigationTargetMasked(surface, 9, "tab-target");
ok(surface?.phase === "target-masked", "the latest request mounts its target under the mask");
surface = settleNavigationSurfaceState(surface, 8);
ok(surface?.intent === 9, "a stale paint terminal cannot release the latest mask");
surface = settleNavigationSurfaceState(surface, 9);
ok(surface === null, "the matching paint terminal releases the mask");

const ticketA1 = createNavigationSurfaceTicket(20, "tab-a", "session-a:1");
const ticketB = createNavigationSurfaceTicket(21, "tab-b", "session-b:1");
const ticketA2 = createNavigationSurfaceTicket(22, "tab-a", "session-a:1");
ok(matchesNavigationSurfaceTicket(ticketA1, ticketA1.token, 20, "tab-a", "session-a:1"), "paint acknowledgement matches the complete ticket");
ok(!matchesNavigationSurfaceTicket(ticketB, ticketA1.token, 21, "tab-b", "session-b:1"), "an old paint token cannot commit B");
ok(!matchesNavigationSurfaceTicket(ticketA2, ticketA1.token, 22, "tab-a", "session-a:1"), "A → B → A cannot revive A's old paint token");
ok(!matchesNavigationSurfaceTicket(ticketA1, ticketA1.token, 20, "tab-a", "session-a:2"), "same-tab replacement session rejects the old ticket");

let paint = advanceSurfacePaintCommit({ attempts: 0, stableFrames: 0 }, {
  rendered: true, placementReady: true, geometryReady: true, geometryKey: "755:1200:445",
});
ok(paint.outcome === undefined, "one paint frame cannot expose the target");
paint = advanceSurfacePaintCommit(paint.progress, {
  rendered: true, placementReady: true, geometryReady: true, geometryKey: "859:1200:341",
});
ok(paint.outcome === undefined, "a changed viewport geometry restarts the stability gate");
paint = advanceSurfacePaintCommit(paint.progress, {
  rendered: true, placementReady: true, geometryReady: true, geometryKey: "859:1200:341",
});
ok(paint.outcome === "ready", "two stable geometry frames commit the target");
let stalled = { attempts: 0, stableFrames: 0 };
let recoveryRequests = 0;
let degraded: string | undefined;
for (let frame = 0; frame < 180; frame += 1) {
  const decision = advanceSurfacePaintCommit(stalled, {
    rendered: true, placementReady: false, geometryReady: false, geometryKey: "755:0:0",
  });
  stalled = decision.progress;
  if (decision.requestRecovery) recoveryRequests += 1;
  degraded = decision.outcome;
}
ok(recoveryRequests === 2, "stalled placement receives two bounded recovery opportunities");
ok(degraded === "degraded", "the second failed placement terminates without permanent loading");

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

const appSource = readFileSync(new URL("../AppRuntime.tsx", import.meta.url), "utf8");
const chatPaneSource = readFileSync(new URL("../app-shell/ChatPaneRegion.tsx", import.meta.url), "utf8");
const appViewSource = readFileSync(new URL("../app-shell/AppRuntimeView.tsx", import.meta.url), "utf8");
const sessionCompositionSource = readFileSync(new URL("../app-runtime/useAppSessionComposition.ts", import.meta.url), "utf8");
const surfaceHookSource = readFileSync(new URL("../lib/useNavigationSurface.ts", import.meta.url), "utf8");
const tabBarSource = readFileSync(new URL("../app-runtime/useTabBarCommands.ts", import.meta.url), "utf8");
const stylesSource = readFileSync(new URL("../styles.css", import.meta.url), "utf8");
ok(surfaceHookSource.includes("flushSync(() => {"), "navigation masking commits synchronously before the Wails await");
ok(surfaceHookSource.includes("setPreserved(rendered?.items.length ? rendered : null)"), "the last stable transcript is retained during navigation");
ok(sessionCompositionSource.includes("visibleTranscriptItems,") && appViewSource.includes("items: session.transcript.visibleTranscriptItems"), "the visible transcript is decoupled from the hydrating target");
ok(chatPaneSource.includes("transcript-navigation-overlay"), "navigation renders a blocking transcript overlay");
ok(/\.transcript-navigation-overlay\s*\{[\s\S]*?background:\s*var\(--chat-bg, var\(--bg\)\)/.test(stylesSource), "the navigation overlay is opaque while target rows settle");
ok(chatPaneSource.includes("live={transitioning ? undefined : state.live}"), "App removes source live output during navigation");
ok(!appSource.includes("hidden={composerSurfaceHidden || undefined}"), "navigation no longer collapses the composer footprint");
// Masked Composer/Todo/rewind layout is exercised through the mounted production
// DecisionFooterRegion in decision-footer-lifecycle.test.tsx, not App source text.
ok(/\.footer--navigation-hidden\s*\{[\s\S]*?visibility:\s*hidden;[\s\S]*?pointer-events:\s*none;/.test(stylesSource), "masked target footer cannot paint or receive input");
ok(appViewSource.includes('style={core.surface.surface?.phase === "source-retained"') && sessionCompositionSource.includes("const visibleDecisionSurface = decisionSurface"), "target-masked paint uses the target footer geometry");
ok((tabBarSource.match(/guardBackendNavigationResult\(\{/g) ?? []).length === 2, "both Reveal paths guard stale backend activation results");
ok(surfaceHookSource.includes("navigation.paint-ready"), "surface settlement is diagnosed only from target paint readiness");

let currentWorkspaceIntent = 30;
let releaseWorkspace!: (picked: string) => void;
const workspaceResult = new Promise<string>((resolve) => { releaseWorkspace = resolve; });
const workspaceCalls: string[] = [];
const staleWorkspace = navigateWorkspace("/workspace-a", {
  claimIntent: () => currentWorkspaceIntent,
  beginSurface: (intent) => workspaceCalls.push(`begin:${intent}`),
  isIntentCurrent: (intent) => intent === currentWorkspaceIntent,
  pickWorkspace: async () => "",
  switchWorkspace: async (path, intent) => {
    workspaceCalls.push(`switch:${intent}:${path}`);
    return workspaceResult;
  },
  markProjectChanged: (updater) => { updater(0); workspaceCalls.push("changed"); },
  refreshTabsAfterMutation: async (latest) => { workspaceCalls.push(`refresh:${latest() ? "current" : "stale"}`); },
  maskTarget: (intent) => workspaceCalls.push(`mask:${intent}`),
});
currentWorkspaceIntent = 31;
releaseWorkspace("/workspace-a");
ok(await staleWorkspace === "/workspace-a", "source workspace data may finish after a newer navigation");
ok(!workspaceCalls.includes("changed") && !workspaceCalls.some((call) => call.startsWith("refresh:")), "stale workspace completion cannot mutate current UI");
ok(workspaceCalls[workspaceCalls.length - 1] === "mask:30", "old workspace finally addresses only its own surface intent");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
