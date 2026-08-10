import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { SkillsSettingsPage } from "../components/CapabilitiesPanel";
import { LocaleProvider } from "../lib/i18n";
import type { AppBindings } from "../lib/bridge";

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
Object.defineProperty(dom.window.HTMLElement.prototype, "attachEvent", { configurable: true, value: () => {} });
Object.defineProperty(dom.window.HTMLElement.prototype, "detachEvent", { configurable: true, value: () => {} });
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.localStorage = dom.window.localStorage;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);

type SkillsView = {
  skills: Array<{ name: string; description: string; scope: string; runAs: string; enabled: boolean }>;
  skillRoots: [];
  allowImplicitInvocation: boolean;
};

const pending: Record<string, Array<(view: SkillsView) => void>> = { a: [], b: [] };
let activeWorkspace = "a";

function view(name: string): SkillsView {
  return {
    skills: [{ name, description: name, scope: "project", runAs: "direct", enabled: true }],
    skillRoots: [],
    allowImplicitInvocation: true,
  };
}

function flushPromises(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 30; attempt += 1) {
    await act(async () => {
      await flushPromises();
    });
    if (predicate()) return;
  }
  throw new Error(`timed out waiting for ${label}`);
}

const appBindings = {
  Meta: async () => ({ eventChannel: "agent:event", cwd: `/workspace-${activeWorkspace}` }),
  ListTabs: async () => [{ id: `tab-${activeWorkspace}`, scope: "project", workspaceRoot: `/workspace-${activeWorkspace}`, active: true }],
  SkillsSettings: async () => new Promise<SkillsView>((resolve) => {
    pending[activeWorkspace].push(resolve);
  }),
} as unknown as AppBindings;

window.go = { main: { App: appBindings } };

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");
const root = createRoot(rootEl);

await act(async () => {
  root.render(
    <LocaleProvider>
      <SkillsSettingsPage activeWorkspaceKey="tab-a\u0000/workspace-a" />
    </LocaleProvider>,
  );
  await flushPromises();
});
await waitFor("workspace A request", () => pending.a.length === 1);

activeWorkspace = "b";
await act(async () => {
  root.render(
    <LocaleProvider>
      <SkillsSettingsPage activeWorkspaceKey="tab-b\u0000/workspace-b" />
    </LocaleProvider>,
  );
  await flushPromises();
});
await waitFor("workspace B request", () => pending.b.length === 1);

await act(async () => {
  pending.b.shift()?.(view("skill-b"));
  await flushPromises();
});
await waitFor("workspace B render", () => rootEl.textContent?.includes("skill-b") === true);

await act(async () => {
  pending.a.shift()?.(view("skill-a"));
  await flushPromises();
});
if (rootEl.textContent?.includes("skill-a")) {
  throw new Error("stale workspace A response overwrote workspace B skills");
}
if (!rootEl.textContent?.includes("skill-b")) {
  throw new Error("workspace B skills disappeared after stale response");
}

await act(async () => {
  root.unmount();
  await flushPromises();
});
console.log("skills settings refresh race: passed");
