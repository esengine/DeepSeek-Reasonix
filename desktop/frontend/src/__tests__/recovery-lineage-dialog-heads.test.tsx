import { JSDOM } from "jsdom";
import type { AppBindings } from "../lib/bridge";
import type { RecoveryCleanupRequest, RecoveryLineageView, RecoveryPreferenceRequest } from "../lib/types";
import { installDesktopHostStub } from "./desktopHostStub";

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", { pretendToBeVisual: true, url: "http://localhost/" });
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(dom.window.navigator, "language", { configurable: true, value: "en-US" });
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.Node = dom.window.Node;
globalThis.Element = dom.window.Element;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.HTMLButtonElement = dom.window.HTMLButtonElement;
globalThis.HTMLInputElement = dom.window.HTMLInputElement;
globalThis.Event = dom.window.Event;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
globalThis.localStorage = dom.window.localStorage;

const chosen: RecoveryPreferenceRequest[] = [];
const renamed: Array<{ path: string; headId: string; name: string }> = [];
const sessionRenames: string[] = [];
const cleanups: RecoveryCleanupRequest[] = [];

// Two heads of one log share the physical path; only the head id tells them apart.
const heads: RecoveryLineageView = {
  groupId: "log",
  state: "heads",
  branchCount: 2,
  unresolved: 0,
  cleanupEligible: 1,
  members: [
    { path: "/private/log.jsonl", headId: "main", headKind: "main", selected: false, role: "normal", versionKind: "head", canonical: false, turns: 2, open: false, running: false, preview: "shared question", versionNote: "log note", lastActivityAt: 100 },
    { path: "/private/log.jsonl", headId: "01HEADFORK", headKind: "fork", headName: "alt", selected: true, role: "normal", versionKind: "head", canonical: true, turns: 3, open: true, running: false, preview: "alt question", versionNote: "alt", lastActivityAt: 200 },
  ],
};

installDesktopHostStub(({
  main: {
    App: {
      ChooseRecoveryBranch: async (request: RecoveryPreferenceRequest) => { chosen.push(request); },
      GetRecoveryLineage: async () => heads,
      RenameSessionHead: async (path: string, headId: string, name: string) => { renamed.push({ path, headId, name }); },
      RenameSession: async (path: string) => { sessionRenames.push(path); },
      CleanRecoveryLineage: async (request: RecoveryCleanupRequest) => {
        cleanups.push(request);
        return { eligible: 1, moved: 1, busy: 0, kept: 0, dryRun: false, items: [{ path: "/private/log.jsonl", headId: "main", status: "retired" }] };
      },
    } as Partial<AppBindings> as AppBindings,
  },
}).main.App);

// React must see the jsdom globals when it loads, or its change-event support
// probe fails and typing never reaches onChange.
const { default: React, act } = await import("react");
const { createRoot } = await import("react-dom/client");
const { RecoveryLineageDialog } = await import("../components/RecoveryLineageDialog");
const { LocaleProvider } = await import("../lib/i18n");
const { ToastProvider } = await import("../lib/toast");

const root = createRoot(document.getElementById("root")!);
await act(async () => {
  root.render(
    <LocaleProvider>
      <ToastProvider>
        <RecoveryLineageDialog topic={{ scope: "global", topicId: "topic" }} initial={heads} onClose={() => {}} onChanged={() => {}} />
      </ToastProvider>
    </LocaleProvider>,
  );
});

const articles = document.querySelectorAll(".recovery-lineage-dialog__member");
if (articles.length !== 2) throw new Error(`expected both heads rendered despite the shared path, got ${articles.length}`);
const body = document.body.textContent || "";
if (!body.includes("alt question") || !body.includes("shared question")) throw new Error("head previews missing");
if (body.includes("/private/") || body.includes("01HEADFORK")) throw new Error("head identity leaked into the dialog");
if (!body.includes("2 versions of this conversation")) throw new Error(`heads summary missing: ${body}`);
if (!body.includes("original line") || !body.includes("forked here")) throw new Error("head kind labels missing");
if (!body.includes("Another version") || !body.includes("alt")) throw new Error("unnamed heads fall back to the version title and named heads show their name");
if (body.includes("unique content") || body.includes("Default version")) throw new Error("file-lineage wording leaked into the heads dialog");

const buttons = () => Array.from(document.querySelectorAll<HTMLButtonElement>("button"));
const chooseButtons = buttons().filter((button) => button.textContent?.trim() === "Make this the current version");
if (chooseButtons.length !== 1) throw new Error(`only the unselected head may offer the choice, got ${chooseButtons.length}`);
await act(async () => { chooseButtons[0].click(); });
await act(async () => { await Promise.resolve(); });
if (chosen.length !== 1 || chosen[0].headId !== "main" || chosen[0].path !== "/private/log.jsonl") {
  throw new Error(`choose did not carry the head id: ${JSON.stringify(chosen)}`);
}

const noteButtons = buttons().filter((button) => button.classList.contains("recovery-lineage-dialog__version-note"));
if (noteButtons.length !== 2) throw new Error(`expected a note editor per head, got ${noteButtons.length}`);
const forkNote = noteButtons.find((button) => button.textContent?.trim() === "alt");
if (!forkNote) throw new Error("fork head note button not found");
await act(async () => { forkNote.click(); });
const editor = () => document.querySelector<HTMLInputElement>(".recovery-lineage-dialog__note-editor input");
if (!editor() || document.querySelectorAll(".recovery-lineage-dialog__note-editor").length !== 1) throw new Error("editing one head must open exactly one editor");
if (editor()!.value !== "alt") throw new Error(`head note editor must start from the head name, got ${editor()!.value}`);
await act(async () => {
  Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, "value")!.set!.call(editor()!, "renamed head");
  editor()!.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
});
const save = buttons().find((button) => button.textContent?.trim() === "Save");
await act(async () => { save!.click(); });
await act(async () => { await Promise.resolve(); });
if (renamed.length !== 1 || renamed[0].headId !== "01HEADFORK" || renamed[0].path !== "/private/log.jsonl" || renamed[0].name !== "renamed head") {
  throw new Error(`head note did not rename the head: ${JSON.stringify(renamed)}`);
}
if (sessionRenames.length !== 0) throw new Error("head note must not rename the whole session");

const retire = buttons().find((button) => button.textContent?.trim() === "Remove covered versions (1)");
if (!retire) throw new Error("covered-head cleanup button missing");
await act(async () => { retire.click(); });
await act(async () => { await Promise.resolve(); });
if (cleanups.length !== 1 || !cleanups[0].apply || cleanups[0].topicId !== "topic" || cleanups[0].scope !== "global") {
  throw new Error(`cleanup did not apply to the topic: ${JSON.stringify(cleanups)}`);
}
const toasts = Array.from(document.querySelectorAll(".toast__text")).map((node) => node.textContent);
if (!toasts.includes("1 covered version(s) removed · 0 still in use")) throw new Error(`cleanup outcome not toasted: ${JSON.stringify(toasts)}`);

await act(async () => root.unmount());
console.log("  PASS  session version dialog lists log heads by head id");
