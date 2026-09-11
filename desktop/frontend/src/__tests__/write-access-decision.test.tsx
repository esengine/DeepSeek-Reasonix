// Run: tsx src/__tests__/write-access-decision.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { ApprovalModal } from "../components/ApprovalModal";
import { LocaleProvider } from "../lib/i18n";
import type { WireApproval } from "../lib/types";

let failed = 0;
function ok(value: unknown, label: string) {
  process.stdout.write(`  ${value ? "PASS" : "FAIL"}  ${label}\n`);
  if (!value) failed += 1;
}
function eq(actual: unknown, expected: unknown, label: string) {
  ok(actual === expected, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}
function flushTimers(ms = 0): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
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
  globalThis.HTMLButtonElement = dom.window.HTMLButtonElement;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.requestAnimationFrame = (callback: FrameRequestCallback) => setTimeout(() => callback(performance.now()), 0) as unknown as number;
  globalThis.cancelAnimationFrame = (id: number) => clearTimeout(id);
  return dom;
}

const dom = installDom();
const root = createRoot(document.getElementById("root")!);
const answers: Array<[boolean, boolean, boolean]> = [];
const approval: WireApproval = {
  id: "write-1",
  tool: "bash",
  subject: "mkdir -p ~/.local/bin && cp tool ~/.local/bin/tool",
  kind: "write_access",
  write_access: {
    directories: ["/Users/me/.local"],
    display_directories: ["~/.local"],
    justification: "install the user-requested local command",
    broad_home_access: true,
    ordinary_permission_needed: true,
    persist_allowed: true,
  },
};
await act(async () => {
  root.render(
    <LocaleProvider>
      <ApprovalModal approval={approval} onAnswer={(a, s, p) => answers.push([a, s, p])} onStop={() => undefined} />
    </LocaleProvider>,
  );
  await flushTimers();
});
ok(document.body.textContent?.includes("Extend write access"), "write-access card uses the directory-expansion title");
ok(document.body.textContent?.includes("~/.local"), "write-access card lists friendly directories");
ok(document.body.textContent?.includes("install the user-requested local command"), "write-access card shows justification");
ok(document.body.textContent?.includes("entire home directory"), "home grant shows the high-risk warning");
ok(document.body.textContent?.includes("also authorizes the current matching"), "merged Ask permission is explained");
const actions = [...document.querySelectorAll(".prompt-shelf__actions .prompt-action")] as HTMLButtonElement[];
eq(actions.length, 4, "write-access approval has four options");
ok(actions[0].textContent?.includes("Allow once"), "first option is allow once");
ok(actions[1].textContent?.includes("this session"), "second option is session grant");
ok(actions[2].textContent?.includes("project"), "third option persists to the project");
ok(actions[3].textContent?.includes("Deny"), "fourth option is deny");
await act(async () => {
  actions[1].click();
  await flushTimers();
});
eq(answers.length, 0, "clicking session only selects");
const confirm = document.querySelector(".decision-confirm-bar__confirm") as HTMLButtonElement;
await act(async () => {
  confirm.click();
  confirm.click();
  await flushTimers(220);
});
eq(answers.length, 1, "write-access confirm submits once");
eq(JSON.stringify(answers[0]), JSON.stringify([true, true, false]), "session maps to (true,true,false)");
await act(async () => root.unmount());
dom.window.close();
if (failed > 0) process.exit(1);
