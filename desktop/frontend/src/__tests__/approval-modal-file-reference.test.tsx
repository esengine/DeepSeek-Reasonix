// Run: tsx src/__tests__/approval-modal-file-reference.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import gsap from "gsap";
import { ApprovalModal } from "../components/ApprovalModal";
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

console.log("\napproval modal file references");

{
  const dom = installDom("en-US");
  mockApp({
    ListDir: async () => [{ name: "src", isDir: true }, { name: "README.md", isDir: false }],
    SearchFileRefs: async () => [],
  });
  const { root, revisions, rerender } = await renderApproval();

  const reviseButton = Array.from(document.querySelectorAll("button")).find((button) => button.textContent?.includes("Revise plan")) as HTMLButtonElement | undefined;
  if (!reviseButton) throw new Error("revise button did not render");

  await act(async () => {
    reviseButton.click();
    await flushTimers();
  });

  const textarea = document.querySelector(".plan-revision__input") as HTMLTextAreaElement | null;
  if (!textarea) throw new Error("plan revision textarea did not render");

  await rerender({ insertRequest: { id: 1, text: "please inspect @" } });
  await waitFor("plan revision @ text opens file suggestions", () => document.body.textContent?.includes("README.md") === true);

  ok(document.body.textContent?.includes("README.md") === true, "plan revision @ text opens file suggestions");

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
      reason: "Windows sandbox failed while starting this command. Run it unconfined one time? This bypasses the OS sandbox for this command only.",
    },
  });

  const text = document.body.textContent ?? "";
  ok(text.includes("仅本次不进沙箱运行：go test ./..."), "sandbox escape approval localizes subject in Chinese UI");
  ok(text.includes("Windows 沙箱启动这条命令时失败"), "sandbox escape approval localizes English backend reason in Chinese UI");
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

  const reviseButton = Array.from(document.querySelectorAll("button")).find((button) => button.textContent?.includes("Revise plan")) as HTMLButtonElement | undefined;
  if (!reviseButton) throw new Error("revise button did not render");

  await act(async () => {
    reviseButton.click();
    await flushTimers();
  });

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
  ok(document.body.textContent?.includes("Hide") === true, "tool approval details start expanded");

  // Consequence preview row: follows the keyboard-selected action by default,
  // arrow navigation, and hover; each pill carries a native title fallback.
  const consequence = () => document.querySelector(".approval-consequence__text")?.textContent ?? null;
  eq(consequence(), "Allow this call only; the next one asks again.", "consequence row previews the selected action by default");

  await act(async () => {
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "ArrowRight", bubbles: true }));
    await flushTimers();
  });
  eq(consequence(), "Allow matching calls until this session ends; resets on restart.", "arrow navigation moves the consequence preview");

  const pills = [...document.querySelectorAll(".prompt-shelf__actions .prompt-action")] as HTMLElement[];
  eq(pills[2]?.getAttribute("title"), "Save a persistent allow rule; future sessions stop asking too.", "action pills carry a native title fallback");
  await act(async () => {
    pills[3].dispatchEvent(new window.MouseEvent("mouseover", { bubbles: true }));
    await flushTimers();
  });
  eq(consequence(), "Reject this call; the model sees the refusal and continues.", "hover overrides the consequence preview");

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
  ok(!text.includes("Allow for session"), "fresh-human approval hides session grant");
  ok(!text.includes("Always allow"), "fresh-human approval hides persistent grant");
  eq(
    Array.from(document.querySelectorAll(".prompt-shelf__actions button")).map((button) => button.textContent).join("|"),
    "1Allow once|2Deny",
    "fresh-human approval keeps conventional allow/deny shortcut keys",
  );

  const allowButton = Array.from(document.querySelectorAll("button")).find((button) => button.textContent?.includes("Allow once")) as HTMLButtonElement | undefined;
  if (!allowButton) throw new Error("allow once button did not render");

  await act(async () => {
    allowButton.click();
    await flushTimers();
  });

  eq(JSON.stringify(answers), JSON.stringify([[true, false, false]]), "fresh-human approval allows only once");

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

  eq(JSON.stringify(answers), JSON.stringify([[false, false, false]]), "fresh-human numeric 2 denies");

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
  ok(!text.includes("Always allow"), "sandbox escape approval hides persistent grant");
  eq(
    Array.from(document.querySelectorAll(".prompt-shelf__actions button")).map((button) => button.textContent).join("|"),
    "1Allow once|2Use real environment for this session|3Deny",
    "sandbox escape approval keeps conventional allow once/session/deny shortcut keys",
  );

  await act(async () => {
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "ArrowRight", bubbles: true, cancelable: true }));
    await flushTimers();
  });
  await act(async () => {
    document.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await flushTimers();
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
  eq(JSON.stringify(answers), JSON.stringify([{ allow: false, session: false, persist: false }]), "sandbox escape numeric 3 denies");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
