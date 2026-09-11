import assert from "node:assert/strict";
import { register } from "node:module";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { initialState } from "../lib/useController";
import { applyHydrateErrorState } from "../lib/hydrateErrorState";
import { projectSessionAvailability, type SessionAvailability } from "../lib/sessionAvailability";
import { useTranscriptSurfaceProjection, type TranscriptSurfaceProjectionInput } from "../app-runtime/useTranscriptSurfaceProjection";
import { projectNavigationSurfaceTarget } from "../app-runtime/conversationProjection";
import { SessionRecoveryBanner } from "../components/SessionRecoveryBanner";
import { LocaleProvider, type Translator } from "../lib/i18n";

register(new URL("../../scripts/svg-loader.mjs", import.meta.url));
const { ChatPaneRegion } = await import("../app-shell/ChatPaneRegion");

const dom = new JSDOM("<div id='root'></div>", { url: "http://localhost" });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, localStorage: dom.window.localStorage,
  IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);
const readyLocal = { ...initialState, meta: { ready: true, eventChannel: "fixture" } };
const failedLocal = applyHydrateErrorState(readyLocal, "startup", "history read failed");
const noop = () => {};
const common: TranscriptSurfaceProjectionInput = {
  hydrating: false, hydrateHistoryLoaded: undefined, hydratePlaceholderItems: undefined, hydratePlaceholderActive: false,
  items: [], remote: false, remoteItems: [], activeTabId: "A", geometrySessionKey: "A", transitioning: false,
  navigationDataReady: true, preserved: null, controllerReady: true,
  availability: projectSessionAvailability({ local: readyLocal }), sessionActivity: false, imDetailActive: false,
  sessionHasContent: false, commitRendered: noop, commitPaint: () => null, commitSingleSurface: noop,
  ports: { loadOlderHistory: async () => false, commitThenSend: async () => {} },
};
let projection!: ReturnType<typeof useTranscriptSurfaceProjection>;
function ProjectionProbe({ input }: { input: TranscriptSurfaceProjectionInput }) {
  projection = useTranscriptSurfaceProjection(input);
  return null;
}
const paintProjection = (input: TranscriptSurfaceProjectionInput) => act(async () => root.render(<ProjectionProbe input={input} />));
try {
  await paintProjection(common);
  assert.equal(projection.emptyHero, true, "successful empty history shows the welcome");
  await paintProjection({ ...common, availability: projectSessionAvailability({ local: failedLocal }) });
  assert.equal(projection.emptyHero, false, "failed history is not a successful empty session");
  for (const state of ["connecting", "reconnecting", "serve_down", "error", "disconnected", "ready"] as const) {
    for (const hydrated of [false, true]) {
      const remote = { state, hydrated, error: "", surfaceGeneration: 1 };
      await paintProjection({ ...common, remote: true, availability: projectSessionAvailability({ local: readyLocal, remote }) });
      assert.equal(projection.emptyHero, state === "ready" && hydrated, `${state}/${hydrated} uses remote readiness`);
    }
  }
  const remoteHistoryFailure = { state: "ready" as const, hydrated: false, error: "snapshot failed", surfaceGeneration: 1 };
  const failedTarget = projectNavigationSurfaceTarget({ activeTabId: "remote", sessionKey: "remote", local: readyLocal, remote: remoteHistoryFailure });
  assert.equal(failedTarget.hydrating, false, "exhausted remote hydration releases the navigation overlay");
  assert.equal(failedTarget.hydrateError, "snapshot failed");
  await paintProjection({ ...common, remote: true, availability: projectSessionAvailability({ remote: remoteHistoryFailure }) });
  assert.equal(projection.emptyHero, false);
  for (const partial of [{ transitioning: true }, { sessionActivity: true }, { hydratePlaceholderActive: true, hydratePlaceholderItems: [] }, { sessionHasContent: true }]) {
    await paintProjection({ ...common, ...partial });
    assert.equal(projection.emptyHero, false, "navigation, decisions, running turns and content keep the main surface visible");
  }

  let retries = 0;
  let rejectRetry!: (error: Error) => void;
  const onRetry = () => { retries++; return new Promise<void>((_resolve, reject) => { rejectRetry = reject; }); };
  const availability: SessionAvailability = { kind: "error", source: "connection", detail: "tunnel closed" };
  const paintBanner = (identity: string, value = availability) => act(async () => root.render(
    <LocaleProvider><SessionRecoveryBanner key={identity} availability={value} onRetry={onRetry} /></LocaleProvider>,
  ));
  await paintBanner("A");
  const button = document.querySelector<HTMLButtonElement>(".session-recovery .btn--primary")!;
  await act(async () => { button.click(); button.click(); });
  assert.equal(retries, 1, "rapid retries share one in-flight action");
  assert.equal(button.disabled, true);
  assert.equal(document.querySelector(".session-recovery")?.getAttribute("role"), "status");
  await act(async () => { rejectRetry(new Error("retry failed")); });
  assert.equal(button.disabled, false, "failed recovery restores the action");
  await act(async () => document.querySelector<HTMLButtonElement>("button[aria-controls]")!.click());
  assert.equal(document.querySelector(".session-recovery__detail")?.textContent, "retry failed");
  await act(async () => button.click());
  await paintBanner("B");
  await act(async () => { rejectRetry(new Error("old session failure")); });
  assert.equal(document.body.textContent?.includes("old session failure"), false, "late recovery cannot overwrite another session's error");
  await paintBanner("B", { kind: "ready", source: "history" });
  assert.equal(document.querySelector(".session-recovery"), null);

  await act(async () => root.render(<LocaleProvider><ChatPaneRegion transitioning={false} t={((key: string) => key) as Translator}
    imDetail={null} transcript={{ state: failedLocal, items: [], tabId: "local", geometrySessionKey: "local", footerHeight: 140,
      transcriptHydrating: false, navigationDataReady: true, readOnly: false, controllerReady: true, hydratePlaceholderActive: false,
      clearContextPending: false, creation: false, availability: projectSessionAvailability({ local: failedLocal }),
      rewind: { stateActive: false, committing: false, signal: undefined }, revealSignal: 0, invocationMetadata: undefined,
      surfaceCommitToken: undefined, liveStore: undefined }} onRetryHistory={async () => { retries++; }}
    commands={{ onPrompt: noop, onDeliveryContinue: noop, onAcceptDelivery: noop, onOpenChanges: noop, onOpenVerification: noop,
      onEditPrompt: noop, onRewind: noop, onLoadOlderHistory: async () => false, onSurfacePaintReady: noop }} /></LocaleProvider>));
  const recovery = document.querySelector(".session-recovery")!;
  assert.ok(recovery, "actual local chat region renders history recovery");
  assert.equal(recovery.closest("main"), null, "history retry is outside the main transcript collapse");
  assert.ok(document.querySelector("main .session-recovery-placeholder"));
  const beforeRetry = retries;
  await act(async () => recovery.querySelector<HTMLButtonElement>(".btn--primary")!.click());
  assert.equal(retries, beforeRetry + 1, "local history recovery calls the owning retry command");
  await act(async () => root.unmount());
  console.log("PASS session recovery: source readiness, navigation errors, visible controls, retry coalescing and stale completion isolation");
} finally { dom.window.close(); }
