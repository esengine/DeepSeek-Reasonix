// Run: tsx src/__tests__/approval-modal-file-reference.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import gsap from "gsap";
import { ApprovalModal } from "../components/ApprovalModal";
import { activeFileReferenceToken, pickInlineFileReference } from "../components/FileReferenceMenu";
import { LocaleProvider } from "../lib/i18n";
import type { AppBindings } from "../lib/bridge";
import type { WireApproval } from "../lib/types";

let passed = 0;
let failed = 0;

type GsapToOptions = { onComplete?: () => void };
const gsapForTests = (typeof gsap.to === "function" ? gsap : (gsap as unknown as { default?: typeof gsap }).default) as unknown as {
  to?: (target: unknown, vars: GsapToOptions) => unknown;
};
if (typeof gsapForTests.to === "function") {
  gsapForTests.to = (_target: unknown, vars: GsapToOptions) => {
    vars.onComplete?.();
    return {};
  };
}

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) ok(true, label);
  else ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

function flushTimers(ms = 0): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function waitFor(label: string, predicate: () => boolean, timeoutMs = 1000) {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    if (predicate()) return;
    await act(async () => {
      await flushTimers(20);
    });
  }
  ok(false, label);
}

function installDom(language = "en-US") {
  const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  Object.defineProperty(dom.window.navigator, "language", { configurable: true, value: language });
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
  globalThis.Node = dom.window.Node;
  globalThis.Element = dom.window.Element;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.HTMLTextAreaElement = dom.window.HTMLTextAreaElement;
  globalThis.Event = dom.window.Event;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.InputEvent = dom.window.InputEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.localStorage = dom.window.localStorage;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  globalThis.getComputedStyle = dom.window.getComputedStyle.bind(dom.window);
  Object.defineProperty(dom.window.HTMLElement.prototype, "attachEvent", { configurable: true, value: () => {} });
  Object.defineProperty(dom.window.HTMLElement.prototype, "detachEvent", { configurable: true, value: () => {} });
  return dom;
}

function mockApp(methods: Partial<AppBindings>) {
  window.go = {
    main: {
      App: {
        ...methods,
        ListDirForTab: methods.ListDirForTab ?? (async (_tabId: string, rel: string) => methods.ListDir?.(rel) ?? []),
        SearchFileRefsForTab: methods.SearchFileRefsForTab ?? (async (_tabId: string, query: string) => methods.SearchFileRefs?.(query) ?? []),
      } as Partial<AppBindings> as AppBindings,
    },
  };
}

async function renderApproval(props: Partial<Parameters<typeof ApprovalModal>[0]> = {}) {
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const revisions: string[] = [];
  const activeStates: boolean[] = [];
  const approval: WireApproval = {
    id: "plan-approval",
    tool: "exit_plan_mode",
    subject: "Plan ready",
  };
  let currentProps: Parameters<typeof ApprovalModal>[0] = {
    approval,
    cwd: "/repo",
    tabId: "tab-a",
    onAnswer: () => undefined,
    onRevisePlan: (text) => revisions.push(text),
    onExitPlan: () => undefined,
    onStop: () => undefined,
    onRevisionActiveChange: (active) => activeStates.push(active),
    ...props,
  };
  const paint = async (nextProps: Partial<Parameters<typeof ApprovalModal>[0]> = {}) => {
    currentProps = { ...currentProps, ...nextProps };
    await act(async () => {
      root.render(
        <LocaleProvider>
          <ApprovalModal {...currentProps} />
        </LocaleProvider>,
      );
      await flushTimers();
    });
  };
  await paint();
  return { root, revisions, activeStates, rerender: paint };
}

function actionButton(label: string): HTMLButtonElement {
  const button = Array.from(document.querySelectorAll(".prompt-shelf__actions .prompt-action")).find((el) =>
    el.textContent?.includes(label),
  ) as HTMLButtonElement | undefined;
  if (!button) throw new Error(`action button not found: ${label}`);
  return button;
}

function confirmButton(): HTMLButtonElement {
  const button = document.querySelector(".decision-confirm-bar__confirm") as HTMLButtonElement | null;
  if (!button) throw new Error("confirm button did not render");
  return button;
}

async function selectAndConfirm(label: string) {
  await act(async () => {
    actionButton(label).click();
    await flushTimers();
  });
  await act(async () => {
    confirmButton().click();
    await flushTimers(220);
  });
}

console.log("\napproval modal file references");

{
  const token = activeFileReferenceToken("please inspect @README\n");
  eq(token?.raw, "README", "plan revision file trigger ignores an invisible trailing newline");
  eq(
    pickInlineFileReference("please inspect @README\n", token?.raw ?? null, token?.dir ?? "", { name: "README.md", isDir: false }),
    "please inspect @README.md ",
    "plan revision file selection removes an invisible trailing newline",
  );
}

{
  const dom = installDom("en-US");
  const fileScopeCalls: string[] = [];
  mockApp({
    ListDirForTab: async (tabId) => {
      fileScopeCalls.push(tabId);
      return [{ name: "src", isDir: true }, { name: "README.md", isDir: false }];
    },
    SearchFileRefsForTab: async () => [],
  });
  const { root, revisions, rerender } = await renderApproval();

  await selectAndConfirm("Revise plan");

  const textarea = document.querySelector(".plan-revision__input") as HTMLTextAreaElement | null;
  if (!textarea) throw new Error("plan revision textarea did not render");

  await rerender({ insertRequest: { id: 1, text: "please inspect @" } });
  await waitFor("plan revision @ text opens file suggestions", () => document.body.textContent?.includes("README.md") === true);

  ok(document.body.textContent?.includes("README.md") === true, "plan revision @ text opens file suggestions");
  ok(fileScopeCalls.every((tabId) => tabId === "tab-a"), "plan revision file suggestions stay scoped to the active tab");

  const readmeButton = Array.from(document.querySelectorAll(".slashmenu__item")).find((button) => button.textContent?.includes("README.md")) as HTMLButtonElement | undefined;
  if (!readmeButton) throw new Error("README file suggestion did not render");

  await act(async () => {
    readmeButton.dispatchEvent(new window.MouseEvent("mousedown", { bubbles: true, cancelable: true }));
    await flushTimers();
  });

  eq(textarea.value, "please inspect @README.md ", "file suggestion completes inline in the plan revision");

  const sendButton = Array.from(document.querySelectorAll("button")).find((button) => button.textContent?.includes("Send update")) as HTMLButtonElement | undefined;
  if (!sendButton) throw new Error("send revision button did not render");

  await act(async () => {
    sendButton.click();
    await flushTimers(220);
  });

  eq(revisions.join(","), "please inspect @README.md", "submitted plan revision keeps the selected file reference");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom("zh-CN");
  mockApp({
    ListDir: async () => [],
    SearchFileRefs: async () => [],
  });
  const { root } = await renderApproval({
    approval: {
      id: "sandbox-escape-approval-zh",
      tool: "sandbox_escape",
      subject: "run unconfined once: go test ./...",
      reason: "Windows does not provide an OS-level Bash sandbox for this command. Run it unconfined one time? This bypasses OS isolation for this command only.",
    },
  });

  const text = document.body.textContent ?? "";
  ok(text.includes("仅本次不进沙箱运行：go test ./..."), "sandbox escape approval localizes subject in Chinese UI");
  ok(text.includes("Windows 不提供这条命令所需的 OS 级 Bash 沙箱"), "sandbox escape approval localizes the retired Windows backend reason in Chinese UI");
  ok(text.includes("允许一次"), "sandbox escape Chinese approval shows allow once");
  ok(text.includes("本会话使用真实环境"), "sandbox escape Chinese approval shows session grant");
  ok(text.includes("拒绝"), "sandbox escape Chinese approval shows deny");
  ok(!text.includes("总是允许"), "sandbox escape Chinese approval hides persistent grant");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom("zh-CN");
  mockApp({
    ListDir: async () => [],
    SearchFileRefs: async () => [],
  });
  const { root } = await renderApproval({
    approval: {
      id: "sandbox-escape-runtime-approval-zh",
      tool: "sandbox_escape",
      subject: "run unconfined once: go test ./...",
      reason: "The OS sandbox could not start this command. Run it unconfined one time? This bypasses OS isolation for this command only.",
    },
  });

  const text = document.body.textContent ?? "";
  ok(text.includes("OS 沙箱无法启动这条命令"), "sandbox escape approval localizes the runtime failure reason in Chinese UI");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom("zh-CN");
  mockApp({
    ListDir: async () => [],
    SearchFileRefs: async () => [],
  });
  const { root } = await renderApproval({
    approval: {
      id: "memory-approval-zh",
      tool: "remember",
      subject: "Save/update memory \"prefers-vitest\" [user]: Preferred test framework | body: Use Vitest for frontend tests.",
    },
  });

  const text = document.body.textContent ?? "";
  ok(text.includes("保存记忆"), "remember approval localizes tool label in Chinese UI");
  ok(text.includes("保存/更新记忆 \"prefers-vitest\" [user]"), "remember approval localizes subject prefix in Chinese UI");
  ok(text.includes("正文: Use Vitest for frontend tests."), "remember approval localizes body label in Chinese UI");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom("zh-CN");
  mockApp({
    ListDir: async () => [],
    SearchFileRefs: async () => [],
  });
  const { root } = await renderApproval({
    approval: {
      id: "plan-mode-read-only-command-zh",
      tool: "plan_mode_read_only_command",
      subject: "Trust \"gh issue view\" as a read-only command prefix while planning\nCommand: gh issue view 5867 --json title",
      reason: "This bash command is not in Reasonix's built-in read-only set. Confirm only if this exact prefix is read-only for planning and research. Auto/YOLO approval cannot answer this trust prompt.",
    },
  });

  const text = document.body.textContent ?? "";
  ok(text.includes("计划模式只读命令"), "plan-mode read-only command approval localizes tool label in Chinese UI");
  ok(text.includes("在计划模式中信任 \"gh issue view\" 为只读命令前缀"), "plan-mode read-only command approval localizes subject in Chinese UI");
  ok(text.includes("不在 Reasonix 内置只读集合中"), "plan-mode read-only command approval localizes reason in Chinese UI");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  mockApp({
    ListDir: async () => [],
    SearchFileRefs: async () => [],
  });
  const { root, activeStates, rerender } = await renderApproval();

  await selectAndConfirm("Revise plan");

  const textarea = document.querySelector(".plan-revision__input") as HTMLTextAreaElement | null;
  if (!textarea) throw new Error("plan revision textarea did not render");

  await rerender({ insertRequest: { id: 2, text: "@src/main.go" } });

  eq(textarea.value, "@src/main.go", "workspace add-reference insert request targets the plan revision input");
  ok(activeStates.includes(true), "plan revision reports itself as the active workspace insertion target");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  mockApp({
    ListDir: async () => [],
    SearchFileRefs: async () => [],
  });
  const { root } = await renderApproval({
    approval: {
      id: "tool-approval",
      tool: "bash",
      subject: "npm run build\n\nRun the build command to verify frontend artifacts.",
    },
  });

  const subject = document.querySelector(".approval-subject");
  ok(subject != null, "tool approval shows its full subject by default");
  eq(
    subject?.textContent,
    "npm run build\n\nRun the build command to verify frontend artifacts.",
    "default-open tool approval keeps the complete subject visible",
  );
  // Subject is always visible; reason expands when short enough / via Details.
  const actions = [...document.querySelectorAll(".prompt-shelf__actions .prompt-action")] as HTMLElement[];
  eq(actions.length, 4, "ordinary tool approval exposes four select-then-confirm options");
  ok(actions[0]?.classList.contains("prompt-action--selected"), "default selection is allow once");
  eq(
    actions[2]?.getAttribute("title"),
    "Save as a persistent matching rule; future sessions stop asking for matching calls.",
    "persistent option carries a native title fallback",
  );
  ok(document.querySelector(".decision-confirm-bar__confirm") != null, "decision surface shows an explicit confirm button");

  await act(async () => {
    actions[2].click();
    await flushTimers();
  });
  ok(actions[2]?.classList.contains("prompt-action--selected"), "clicking an option only changes selection");
  eq(
    document.querySelectorAll(".prompt-action--selected").length >= 1,
    true,
    "selection state updates without submitting",
  );

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  mockApp({
    ListDir: async () => [],
    SearchFileRefs: async () => [],
  });
  const answers: Array<[boolean, boolean, boolean]> = [];
  const { root } = await renderApproval({
    approval: {
      id: "memory-approval",
      tool: "remember",
      subject: "Save/update memory \"prefers-vitest\": Preferred test framework",
    },
    onAnswer: (allow, session, persist) => answers.push([allow, session, persist]),
  });

  const text = document.body.textContent ?? "";
  ok(text.includes("Allow once"), "fresh-human approval shows allow once");
  ok(text.includes("Deny"), "fresh-human approval shows deny");
  ok(!text.includes("Allow matching for this session"), "fresh-human approval hides session grant");
  ok(!text.includes("Always allow matching"), "fresh-human approval hides persistent grant");
  eq(
    Array.from(document.querySelectorAll(".prompt-shelf__actions button")).map((button) => button.textContent).join("|"),
    "1Allow onceAllow this call only; the next one asks again.|2DenyReject this call; the model sees the refusal and continues.",
    "fresh-human approval keeps conventional allow/deny shortcut keys with inline consequences",
  );

  await act(async () => {
    actionButton("Allow once").click();
    await flushTimers();
  });
  eq(JSON.stringify(answers), "[]", "clicking allow once only selects; does not approve yet");

  await act(async () => {
    confirmButton().click();
    await flushTimers(220);
  });

  eq(JSON.stringify(answers), JSON.stringify([[true, false, false]]), "fresh-human approval allows only once after confirm");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  mockApp({
    ListDir: async () => [],
    SearchFileRefs: async () => [],
  });
  const answers: Array<[boolean, boolean, boolean]> = [];
  const { root } = await renderApproval({
    approval: {
      id: "memory-approval-deny",
      tool: "remember",
      subject: "Save/update memory \"prefers-vitest\": Preferred test framework",
    },
    onAnswer: (allow, session, persist) => answers.push([allow, session, persist]),
  });

  await act(async () => {
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "2", bubbles: true, cancelable: true }));
    await flushTimers();
  });
  eq(JSON.stringify(answers), "[]", "fresh-human numeric 2 only selects deny");

  await act(async () => {
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await flushTimers(220);
  });

  eq(JSON.stringify(answers), JSON.stringify([[false, false, false]]), "fresh-human Enter after digit 2 denies");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  mockApp({
    ListDir: async () => [],
    SearchFileRefs: async () => [],
  });
  const answers: Array<{ allow: boolean; session: boolean; persist: boolean }> = [];
  const { root } = await renderApproval({
    approval: {
      id: "sandbox-escape-approval",
      tool: "sandbox_escape",
      subject: "run unconfined once: go test ./...",
      reason: "Windows sandbox failed while starting this command. Run it unconfined one time? This bypasses the OS sandbox for this command only.",
    },
    onAnswer: (allow, session, persist) => answers.push({ allow, session, persist }),
  });

  const text = document.body.textContent ?? "";
  ok(text.includes("bash sandbox escape"), "sandbox escape approval uses a clear tool label");
  ok(text.includes("Allow once"), "sandbox escape approval shows allow once");
  ok(text.includes("Use real environment for this session"), "sandbox escape approval shows session grant");
  ok(text.includes("Deny"), "sandbox escape approval shows deny");
  ok(!text.includes("Always allow matching"), "sandbox escape approval hides persistent grant");
  eq(document.querySelectorAll(".prompt-shelf__actions .prompt-action").length, 3, "sandbox escape keeps three options");

  await act(async () => {
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true, cancelable: true }));
    await flushTimers();
  });
  await act(async () => {
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await flushTimers(220);
  });
  eq(JSON.stringify(answers), JSON.stringify([{ allow: true, session: true, persist: false }]), "sandbox escape Enter on selected session action grants session");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  mockApp({
    ListDir: async () => [],
    SearchFileRefs: async () => [],
  });
  const answers: Array<{ allow: boolean; session: boolean; persist: boolean }> = [];
  const { root } = await renderApproval({
    approval: {
      id: "sandbox-escape-deny-approval",
      tool: "sandbox_escape",
      subject: "run unconfined once: go test ./...",
      reason: "Windows sandbox failed while starting this command. Run it unconfined one time? This bypasses the OS sandbox for this command only.",
    },
    onAnswer: (allow, session, persist) => answers.push({ allow, session, persist }),
  });

  await act(async () => {
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "3", bubbles: true, cancelable: true }));
    await flushTimers();
  });
  eq(JSON.stringify(answers), "[]", "sandbox escape numeric 3 only selects deny");
  await act(async () => {
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await flushTimers(220);
  });
  eq(JSON.stringify(answers), JSON.stringify([{ allow: false, session: false, persist: false }]), "sandbox escape Enter after digit 3 denies");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom("en-US");
  const pending: Array<(entries: Array<{ name: string; isDir: boolean }>) => void> = [];
  mockApp({
    ListDirForTab: async () => new Promise((resolve) => pending.push(resolve)),
    SearchFileRefsForTab: async () => [],
  });
  const { root, rerender } = await renderApproval({ workspaceScopeKey: "session-a" });

  await selectAndConfirm("Revise plan");
  await rerender({ insertRequest: { id: 20, text: "inspect @" } });
  await waitFor("initial approval session scope request", () => pending.length === 1);
  await rerender({ workspaceScopeKey: "session-b" });
  await waitFor("next approval session scope request", () => pending.length === 2);

  await act(async () => {
    pending[1]([{ name: "current-plan-file.ts", isDir: false }]);
    await flushTimers();
  });
  await waitFor("current approval session file result", () => document.body.textContent?.includes("current-plan-file.ts") === true);

  await act(async () => {
    pending[0]([{ name: "stale-plan-file.ts", isDir: false }]);
    await flushTimers();
  });
  ok(document.body.textContent?.includes("current-plan-file.ts") === true, "current approval session file refs stay visible");
  ok(document.body.textContent?.includes("stale-plan-file.ts") === false, "late approval session file refs are ignored");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
