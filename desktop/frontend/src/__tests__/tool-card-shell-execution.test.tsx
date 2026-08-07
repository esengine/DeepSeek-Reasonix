// Run: tsx src/__tests__/tool-card-shell-execution.test.tsx
//
// Desktop ToolCard shell-execution presentation for the three host paths:
//   1) live tool_result event (reducer → card)
//   2) history recovery (historyMessagesToItems)
//   3) archived expand (ToolResultForTab injects execution back onto fullData)

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import gsap from "gsap";
import { ToolCard } from "../components/ToolCard";
import { LocaleProvider } from "../lib/i18n";
import { historyMessagesToItems, initialState, reducer, type Item } from "../lib/useController";
import type { HistoryMessage, WireShellExecution } from "../lib/types";

type ToolItem = Extract<Item, { kind: "tool" }>;

// jsdom has no layout engine: stub GSAP the same way as subagent-progress-card.
type GsapToOptions = { onComplete?: () => void };
const gsapForTests = gsap as unknown as {
  to: (target: unknown, vars: GsapToOptions) => unknown;
  fromTo: (target: unknown, from: unknown, vars: GsapToOptions) => unknown;
  set: (target: unknown, vars: unknown) => unknown;
  killTweensOf: (target: unknown) => void;
};
gsapForTests.to = (_target: unknown, vars: GsapToOptions) => {
  vars.onComplete?.();
  return {};
};
gsapForTests.fromTo = (_target: unknown, _from: unknown, vars: GsapToOptions) => {
  vars.onComplete?.();
  return {};
};
gsapForTests.set = () => ({});
gsapForTests.killTweensOf = () => {};

let passed = 0;
let failed = 0;

function ok(value: unknown, label: string) {
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

function flushTimers(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

function installDom() {
  const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
  globalThis.Node = dom.window.Node;
  globalThis.Element = dom.window.Element;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.Event = dom.window.Event;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  dom.window.matchMedia = () => ({
    matches: true,
    media: "(prefers-reduced-motion: reduce)",
    onchange: null,
    addListener: () => undefined,
    removeListener: () => undefined,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    dispatchEvent: () => false,
  });
  return dom;
}

async function renderCard(item: ToolItem, tabId?: string) {
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  await act(async () => {
    root.render(
      React.createElement(LocaleProvider, null,
        React.createElement(ToolCard, { item, tabId }),
      ),
    );
    await flushTimers();
  });
  return {
    root,
    dom,
    async expand() {
      const head = document.querySelector(".tool__head") as HTMLButtonElement | null;
      if (!head) throw new Error("tool head missing");
      await act(async () => {
        head.click();
        await flushTimers();
      });
    },
    async cleanup() {
      await act(async () => {
        root.unmount();
      });
      dom.window.close();
    },
  };
}

const failedPSExecution: WireShellExecution = {
  kind: "shell",
  shell: "powershell",
  shellVersion: "5.1",
  platform: "windows",
  supportsAndAnd: false,
  state: "failed",
  failurePhase: "execution",
  exitCode: 1,
  outputTail: "Select-String : 找不到路径“C:\\中文\\app.ps1”。\nAt line:1 char:1",
  mutationRisk: "may_be_partial",
  verification: "not_verification",
  durationMs: 42,
};

const preflightExecution: WireShellExecution = {
  kind: "shell",
  shell: "bash",
  state: "not_run",
  failurePhase: "preflight",
  mutationRisk: "not_started",
  verification: "not_run",
  durationMs: 0,
};

console.log("\ntool card shell execution");

// ── Path 1: live tool_result event ──
{
  let s = reducer(initialState, { type: "event", e: { kind: "turn_started" } });
  s = reducer(s, {
    type: "event",
    e: {
      kind: "tool_dispatch",
      tool: {
        id: "live-ps",
        name: "bash",
        args: `{"command":"Get-Content .\\\\中文\\\\app.ps1"}`,
        readOnly: false,
      },
    },
  });
  s = reducer(s, {
    type: "event",
    e: {
      kind: "tool_result",
      tool: {
        id: "live-ps",
        name: "bash",
        readOnly: false,
        output: "error: command exited: exit status 1\nSelect-String failed",
        err: "command exited: exit status 1",
        durationMs: 42,
        execution: failedPSExecution,
      },
    },
  });
  const item = s.items.find((it): it is ToolItem => it.kind === "tool" && it.id === "live-ps");
  ok(!!item, "live path: tool item created");
  eq(item?.isShell, true, "live path: bash result marked isShell");
  eq(item?.execution?.shell, "powershell", "live path: execution.shell preserved");
  eq(item?.execution?.exitCode, 1, "live path: exitCode preserved");
  eq(item?.execution?.failurePhase, "execution", "live path: failurePhase preserved");

  const ui = await renderCard(item!);
  const name = document.querySelector(".tool__name")?.textContent ?? "";
  ok(name.includes("Windows PowerShell"), `live path: card shows Windows PowerShell (got ${JSON.stringify(name)})`);
  const duration = document.querySelector(".tool__duration")?.textContent ?? "";
  ok(duration.includes("exit 1") || duration.includes("execution"), `live path: summary shows exit/phase (got ${JSON.stringify(duration)})`);
  const risk = document.body.textContent ?? "";
  ok(risk.includes("partially modified") || risk.includes("部分"), "live path: partial mutation risk visible");
  // Execution failure must not claim the command never ran.
  ok(
    !(risk.includes("did not run") || risk.includes("command did not run") || risk.includes("命令未执行") || risk.includes("未执行")),
    "live path: execution failure must not show not-run label",
  );

  await ui.expand();
  const details = document.querySelector("details.tool__error-details, .tool__error-details, details");
  ok(!!details, "live path: stderr details element present after expand affordance");
  if (details) {
    await act(async () => {
      details.setAttribute("open", "");
      details.dispatchEvent(new Event("toggle"));
      await flushTimers();
    });
  }
  const afterExpand = document.body.textContent ?? "";
  ok(
    afterExpand.includes("中文") || afterExpand.includes("找不到路径"),
    "live path: Chinese stderr from execution.outputTail is visible in the DOM",
  );

  await ui.cleanup();
}

// timed_out / cancelled must surface partial-write risk (backend may_be_partial).
{
  for (const state of ["timed_out", "cancelled"] as const) {
    const item: ToolItem = {
      kind: "tool",
      id: `risk-${state}`,
      name: "bash",
      args: `{"command":"long-run"}`,
      readOnly: false,
      status: "error",
      error: state === "timed_out" ? "command timed out" : "context canceled",
      isShell: true,
      execution: {
        kind: "shell",
        shell: "bash",
        state,
        failurePhase: state === "timed_out" ? "timeout" : "cancellation",
        mutationRisk: "may_be_partial",
        verification: "not_verification",
        durationMs: 100,
      },
    };
    const ui = await renderCard(item);
    const body = document.body.textContent ?? "";
    ok(
      body.includes("partially modified") || body.includes("部分"),
      `${state}: shows partial mutation risk`,
    );
    ok(
      !(body.includes("did not run") || body.includes("命令未执行")),
      `${state}: does not claim command never ran`,
    );
    await ui.cleanup();
  }
}

// ── Path 2: history recovery ──
{
  const messages: HistoryMessage[] = [
    {
      role: "assistant",
      content: "",
      toolCalls: [{ id: "hist-bash", name: "bash", arguments: "{\"command\":\"go test ./...\"}" }],
    },
    {
      role: "tool",
      content: "blocked: mixed mutation and verification",
      toolCallId: "hist-bash",
      toolName: "bash",
      toolResultError: "blocked: mixed mutation and verification command",
      execution: preflightExecution,
    },
  ];
  const items = historyMessagesToItems(messages, "h").items.filter((it): it is ToolItem => it.kind === "tool");
  eq(items.length, 1, "history path: one tool item");
  eq(items[0]?.isShell, true, "history path: bash isShell");
  eq(items[0]?.execution?.failurePhase, "preflight", "history path: execution restored");
  eq(items[0]?.execution?.mutationRisk, "not_started", "history path: not_started risk");

  const ui = await renderCard(items[0]!);
  const name = document.querySelector(".tool__name")?.textContent ?? "";
  ok(name === "bash" || name.includes("bash"), `history path: shell name bash (got ${JSON.stringify(name)})`);
  const body = document.body.textContent ?? "";
  ok(
    body.includes("did not run") || body.includes("not run") || body.includes("未执行") || body.includes("命令未执行"),
    "history path: preflight shows command-not-run (not partial)",
  );
  ok(
    !(body.includes("partially modified") || body.includes("部分修改文件")),
    "history path: preflight must not claim partial file modification",
  );
  await ui.cleanup();
}

// ── Path 3a: archive compaction keeps execution from the live result ──
{
  let s = reducer(initialState, { type: "event", e: { kind: "turn_started" } });
  s = reducer(s, {
    type: "event",
    e: {
      kind: "tool_dispatch",
      tool: { id: "arch-live", name: "bash", args: `{"command":"exit 1"}`, readOnly: false },
    },
  });
  s = reducer(s, {
    type: "event",
    e: {
      kind: "tool_result",
      tool: {
        id: "arch-live",
        name: "bash",
        readOnly: false,
        output: "error: exit 1\n" + "y".repeat(5000),
        err: "command exited: exit status 1",
        durationMs: 42,
        execution: failedPSExecution,
      },
    },
  });
  const item = s.items.find((it): it is ToolItem => it.kind === "tool" && it.id === "arch-live");
  ok(!!item?.dataArchived, "archive path: tool_result archives large output");
  eq(item?.output, undefined, "archive path: output dropped after archive");
  eq(item?.execution?.shell, "powershell", "archive path: execution survives compactArchivedToolItems");
  eq(item?.execution?.exitCode, 1, "archive path: exitCode survives archive");

  const ui = await renderCard(item!);
  ok((document.querySelector(".tool__name")?.textContent ?? "").includes("Windows PowerShell"),
    "archive path: card still shows Windows PowerShell without re-fetch");
  ok((document.body.textContent ?? "").includes("partially modified") || (document.body.textContent ?? "").includes("部分"),
    "archive path: partial risk still visible when execution retained");
  await ui.cleanup();
}

// ── Path 3b: ToolResultForTab rehydrates execution when only fullData has it ──
{
  const archived: ToolItem = {
    kind: "tool",
    id: "arch-ps",
    name: "bash",
    args: "",
    readOnly: false,
    status: "error",
    error: "command exited: exit status 1",
    dataArchived: true,
    isShell: true,
    execution: undefined,
    durationMs: 42,
  };

  const dom = installDom();
  // Inject a Wails-shaped binding so bridge.realApp() picks up our stub
  // instead of the browser mock (which always returns null for ToolResultForTab).
  (window as unknown as { go: { main: { App: Record<string, unknown> } } }).go = {
    main: {
      App: {
        ToolResultForTab: async () => ({
          args: `{"command":"Get-Content .\\\\中文\\\\app.ps1"}`,
          output: "error: command exited: exit status 1\nSelect-String failed",
          execution: failedPSExecution,
        }),
      },
    },
  };
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);

  try {
    await act(async () => {
      root.render(
        React.createElement(LocaleProvider, null,
          React.createElement(ToolCard, { item: archived, tabId: "tab-1" }),
        ),
      );
      await flushTimers();
    });
    const head = document.querySelector(".tool__head") as HTMLButtonElement | null;
    if (!head) throw new Error("tool head missing");
    await act(async () => {
      head.click();
      await flushTimers();
      await flushTimers();
      await flushTimers();
    });

    const name = document.querySelector(".tool__name")?.textContent ?? "";
    ok(name.includes("Windows PowerShell"), `archive rehydrate: shell name after expand (got ${JSON.stringify(name)})`);
    const duration = document.querySelector(".tool__duration")?.textContent ?? "";
    ok(
      duration.includes("exit 1") || duration.includes("execution"),
      `archive rehydrate: exit/phase after expand (got ${JSON.stringify(duration)})`,
    );
    const body = document.body.textContent ?? "";
    ok(body.includes("partially modified") || body.includes("部分"), "archive rehydrate: partial risk after expand");
  } finally {
    await act(async () => {
      root.unmount();
    });
    delete (window as unknown as { go?: unknown }).go;
    dom.window.close();
  }
}

// ── Nil execution safety ──
{
  const plain: ToolItem = {
    kind: "tool",
    id: "plain",
    name: "bash",
    args: `{"command":"echo hi"}`,
    readOnly: false,
    status: "done",
    output: "hi\n",
    isShell: true,
  };
  const ui = await renderCard(plain);
  ok(document.querySelector(".tool__name")?.textContent === "bash", "nil execution falls back to bash label");
  ok(!document.querySelector("[data-shell]") || document.querySelector("[data-shell]")?.getAttribute("data-shell") === "bash",
    "nil execution still renders shell card without throwing");
  await ui.cleanup();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
