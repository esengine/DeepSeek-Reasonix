import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { Composer } from "../components/Composer";
import { LocaleProvider } from "../lib/i18n";
import { ToastProvider } from "../lib/toast";
import { installDesktopHostStub } from "./desktopHostStub";

const dom = new JSDOM("<div id='root'></div>", { url: "http://localhost/", pretendToBeVisual: true });
Object.assign(globalThis, {
  window: dom.window,
  document: dom.window.document,
  localStorage: dom.window.localStorage,
  IS_REACT_ACT_ENVIRONMENT: true,
  requestAnimationFrame: dom.window.requestAnimationFrame.bind(dom.window),
  cancelAnimationFrame: dom.window.cancelAnimationFrame.bind(dom.window),
  ResizeObserver: class { observe() {} disconnect() {} unobserve() {} },
});
for (const name of ["Node", "HTMLElement", "HTMLTextAreaElement", "Event", "CustomEvent", "File", "FileReader", "MutationObserver"]) {
  Object.defineProperty(globalThis, name, { configurable: true, value: Reflect.get(dom.window, name) });
}
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
Object.defineProperty(dom.window.HTMLElement.prototype, "attachEvent", { configurable: true, value() {} });
Object.defineProperty(dom.window.HTMLElement.prototype, "detachEvent", { configurable: true, value() {} });
window.matchMedia = (() => ({ matches: true, addEventListener() {}, removeEventListener() {} })) as typeof window.matchMedia;

installDesktopHostStub({
  Commands: async () => [],
  Models: async () => [],
  ModelsForTab: async () => [],
  ListDir: async () => [],
  ListDirForTab: async () => [],
  SearchFileRefs: async () => [],
  SearchFileRefsForTab: async () => [],
  StageImageForTab: async () => ({ draftId: "0123456789abcdef0123456789abcdef", displayName: "draft.png", mime: "image/png", width: 1, height: 1, bytes: 12 }),
  CaptureAttachmentTarget: async () => ({token:"target-a",capabilities:["attachments-v2"]}),
  ReleaseAttachmentTarget: async () => {},
  StartTurnForAttachmentTarget: async () => ({turnId:"turn",status:"queued",submissionId:"submission"}),
  StageImageForTarget: async () => ({ draftId: "0123456789abcdef0123456789abcdef", displayName: "draft.png", mime: "image/png", width: 1, height: 1, bytes: 12 }),
  ReadDraftImageForTarget: async () => "data:image/png;base64,iVBORw0KGgo=",
  ReadDraftImageForTab: async () => "data:image/png;base64,iVBORw0KGgo=",
  SavePastedImageForTab: async () => ".reasonix/attachments/draft.png",
  AttachmentDataURLForTab: async () => "data:image/png;base64,iVBORw0KGgo=",
});

const root = createRoot(document.getElementById("root")!);
const flush = () => new Promise<void>((resolve) => setTimeout(resolve, 0));
try {
  await act(async () => {
    root.render(<LocaleProvider><ToastProvider><Composer
      running={false} collaborationMode="normal" toolApprovalMode="ask" goal="" cwd="/repo"
      modelLabel="DeepSeek-R1" tabId="tab-a" sessionKey="session-a" ready
      insertRequest={{ id: 1, text: "keep this draft", mode: "replace" }}
      onSend={async () => { throw new Error("reasonix_error:image_attachment_unreadable"); }}
      onCancel={async () => ({ discardedItemIds: [] })} onCycleMode={() => {}} onSetMode={() => {}}
      onSetCollaborationMode={() => {}} onSetToolApprovalMode={() => {}} onClearGoal={() => {}}
      onSwitchModel={() => {}} onSetEffort={() => {}}
    /></ToastProvider></LocaleProvider>);
    await flush();
  });
  const textarea = document.querySelector<HTMLTextAreaElement>("textarea")!;
  const file = new File(["draft image"], "draft.png", { type: "image/png", lastModified: 1 });
  const paste = new Event("paste", { bubbles: true, cancelable: true });
  Object.defineProperty(paste, "clipboardData", {
    configurable: true,
    value: { files: [file], items: [], types: [], getData: () => "" },
  });
  await act(async () => { textarea.dispatchEvent(paste); await flush(); });
  for (let attempt = 0; attempt < 10 && !document.querySelector(".composer-context__item"); attempt += 1) {
    await act(async () => flush());
  }
  assert.equal(document.querySelectorAll(".composer-context__item").length, 1);
  await act(async () => { document.querySelector<HTMLButtonElement>(".composer__btn--send")!.click(); await flush(); });
  assert.equal(textarea.value, "keep this draft");
  assert.equal(document.querySelectorAll(".composer-context__item").length, 1);
  assert.match(document.body.textContent ?? "", /The image could not be read\. Re-add it or try again/);
  console.log("composer image admission: rejected image keeps text and attachment for retry");
} finally {
  await act(async () => root.unmount());
  dom.window.close();
}
