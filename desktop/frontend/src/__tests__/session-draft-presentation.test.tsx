import assert from "node:assert/strict";
import { register } from "node:module";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";

import type { SessionDraftSurface } from "../app-runtime/useSessionDraftSurface";
import { initialState } from "../lib/useController";
import { LocaleProvider, type Translator } from "../lib/i18n";

register(new URL("../../scripts/svg-loader.mjs", import.meta.url));
const { ChatPaneRegion } = await import("../app-shell/ChatPaneRegion");
const { DraftTopicbarActions } = await import("../app-shell/DraftTopicbarActions");

const dom = new JSDOM("<div id='root'></div>", { url: "http://localhost" });
Object.assign(globalThis, {
  window: dom.window,
  document: dom.window.document,
  localStorage: dom.window.localStorage,
  IS_REACT_ACT_ENVIRONMENT: true,
});

const surface: SessionDraftSurface = {
  kind: "draft",
  draft: {
    id: "draft-project",
    workspaceId: "workspace-project",
    scope: "project",
    workspaceRoot: "/workspace/project",
    revision: 1,
    contentJson: "{}",
    settings: { model: "fixture/model", mode: "normal", toolApprovalMode: "ask", disabledMcp: {}, mcpOrder: [] },
    status: "active",
    updatedAt: 1,
  },
  content: { text: "", invocations: [], attachments: [], workspaceRefs: [], pastedBlocks: [], openPastedLabels: [], sessionRefs: [], selectedTextRefs: [] },
  settings: { model: "fixture/model", mode: "normal", toolApprovalMode: "ask", disabledMcp: {}, mcpOrder: [] },
  commands: [],
  servers: [],
  generation: 1,
  editVersion: 0,
  pendingTasks: 0,
  preparingSubmission: false,
  saveState: "saved",
};

const noop = () => {};
const commands = { onPrompt: noop, onFork: noop, onLoadOlderHistory: async () => false, onLoadNewerHistory: async () => false, onSurfacePaintReady: noop };
const transcript = {
  state: { ...initialState, meta: { ready: true, eventChannel: "fixture" } },
  items: [], tabId: undefined, geometrySessionKey: "draft", footerHeight: 0,
  invocationMetadata: undefined, surfaceCommitToken: undefined, liveStore: undefined,
  transcriptHydrating: false, navigationDataReady: true, readOnly: false, controllerReady: false,
  hydratePlaceholderActive: false, clearContextPending: false, availability: { kind: "ready", source: "history" } as const,
  rewind: { stateActive: false, committing: false },
};
const draftActions = {
  surface,
  onUseSaved: noop,
  onKeepLocal: noop,
  onRetrySave: noop,
  onDismissTaskError: noop,
  onResume: noop,
  onOpenSession: noop,
  onCheckSubmission: noop,
};

const root = createRoot(document.getElementById("root")!);
try {
  await act(async () => root.render(<LocaleProvider><ChatPaneRegion
    transitioning={false} t={((key: string) => key) as Translator} imDetail={null} remote={undefined}
    draft={draftActions} transcript={transcript} onRetryHistory={async () => {}} commands={commands}
  /></LocaleProvider>));
  assert.ok(document.querySelector(".main--draft-landing"), "a healthy draft uses the clean new-session landing surface");
  assert.equal(document.querySelector(".session-draft-surface"), null, "healthy drafts do not render the management page");

  await act(async () => root.render(<LocaleProvider><ChatPaneRegion
    transitioning={false} t={((key: string) => key) as Translator} imDetail={null} remote={undefined}
    draft={{ ...draftActions, surface: { ...surface, saveState: "conflict", conflict: surface.draft } }}
    transcript={transcript} onRetryHistory={async () => {}} commands={commands}
  /></LocaleProvider>));
  assert.ok(document.querySelector(".session-draft-attention[role=alert]"), "a conflict keeps its recovery actions visible");
  assert.equal(document.querySelectorAll(".session-draft-attention button").length, 2, "both conflict resolutions remain available");

  let discarded = 0;
  const mcpSelections: boolean[] = [];
  const server = {
    name: "fixture-mcp", transport: "stdio", status: "deferred", enabled: true, installed: true,
    autoStart: true, tools: 0, toolCount: 0, prompts: 0, resources: 0, toolList: [],
  };
  await act(async () => root.render(<LocaleProvider><DraftTopicbarActions
    t={((key: string) => key) as Translator}
    draft={{ ...surface, servers: [server] }}
    onSetMCPEnabled={(_server, enabled) => mcpSelections.push(enabled)}
    onDiscard={() => { discarded++; }}
  /></LocaleProvider>));
  assert.equal(document.querySelector(".draft-topicbar-mcp summary")?.textContent, "1/1", "the compact control preserves MCP visibility");
  await act(async () => document.querySelector<HTMLInputElement>(".draft-topicbar-mcp input")!.click());
  await act(async () => document.querySelector<HTMLButtonElement>('button[aria-label="draft.discard"]')!.click());
  assert.deepEqual(mcpSelections, [false], "the compact control preserves per-draft MCP selection");
  assert.equal(discarded, 1, "the compact control preserves draft discard");

  await act(async () => root.unmount());
  console.log("PASS session draft presentation: clean landing and exceptional recovery controls");
} finally {
  dom.window.close();
}
