import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { DiagnosticsSettingsPage } from "../components/DiagnosticsSettingsPage";
import type { AppBindings } from "../lib/bridge";
import { LocaleProvider } from "../lib/i18n";
import type { CapabilityDiagnosticsReport, SettingsTab } from "../lib/types";

function ok(value: unknown, message: string) {
  if (!value) throw new Error(message);
}

function flush(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 40; attempt += 1) {
    await act(async () => {
      await flush();
    });
    if (predicate()) return;
  }
  throw new Error(`timed out waiting for ${label}: ${document.body?.textContent?.slice(0, 400) ?? ""}`);
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
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.HTMLButtonElement = dom.window.HTMLButtonElement;
  globalThis.HTMLInputElement = dom.window.HTMLInputElement;
  globalThis.Event = dom.window.Event;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.localStorage = dom.window.localStorage;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    value: () => ({
      matches: true,
      media: "(prefers-reduced-motion: reduce)",
      onchange: null,
      addEventListener() {},
      removeEventListener() {},
      addListener() {},
      removeListener() {},
      dispatchEvent: () => false,
    }),
  });
  return dom;
}

function baseReport(runtime: boolean): CapabilityDiagnosticsReport {
  return {
    schema_version: 1,
    root: "<workspace>",
    live: false,
    summary: {
      errors: 1,
      warnings: 1,
      infos: runtime ? 1 : 0,
      instructions: 1,
      skills: 1,
      commands: 0,
      hooks: 0,
      plugins: 0,
      mcp_servers: 1,
    },
    instructions: { docs: [{ path: "<workspace>/AGENTS.md", scope: "project", order: 1 }] },
    skills: {
      roots: [{ path: "<workspace>/.reasonix/skills", scope: "project", status: "ok" }],
      entries: [{ name: "demo", path: "<workspace>/.reasonix/skills/demo/SKILL.md", status: "winner" }],
      winners: 1,
      shadowed: 0,
    },
    commands: { roots: [], entries: [], winners: 0, shadowed: 0 },
    hooks: { trusted_project: false, project_defines_hooks: true, sources: [], entries: [] },
    plugins: { packages: [] },
    mcp: {
      servers: [{
        name: "demo-mcp",
        transport: "stdio",
        start_intent: "automatic",
        source: "toml",
        runtime_status: runtime ? "connected" : undefined,
        tool_count: runtime ? 2 : undefined,
      }],
    },
    issues: [
      {
        severity: "error",
        code: "mcp.command_not_found",
        subsystem: "mcp",
        name: "demo-mcp",
        message: "command missing",
        remediation: "install binary",
        settings_tab: "mcp",
      },
      {
        severity: "warning",
        code: "skill.missing_description",
        subsystem: "skills",
        name: "nodesc",
        message: "no description",
        settings_tab: "skills",
      },
    ],
  };
}

console.log("diagnostics settings page");

{
  const calls: boolean[] = [];
  const navigations: SettingsTab[] = [];
  installDom();
  // Prefer English labels for stable button text assertions.
  window.localStorage.setItem("reasonix-lang", "en");

  window.go = {
    main: {
      App: {
        CapabilityDiagnostics: async (includeSessionRuntime: boolean) => {
          calls.push(includeSessionRuntime);
          return baseReport(includeSessionRuntime);
        },
      } as Partial<AppBindings> as AppBindings,
    },
  };

  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);

  await act(async () => {
    root.render(
      React.createElement(
        LocaleProvider,
        null,
        React.createElement(DiagnosticsSettingsPage, {
          onNavigate: (tab: SettingsTab) => {
            navigations.push(tab);
          },
        }),
      ),
    );
    await flush();
  });

  await waitFor("static report", () => (rootEl.textContent || "").includes("mcp.command_not_found"));
  ok(calls[0] === false, "initial load must request static report (includeSessionRuntime=false)");
  ok((rootEl.textContent || "").includes("skill.missing_description"), "warnings must render");
  ok(rootEl.querySelector(".diag-summary"), "health summary must render");

  const runtimeToggle = rootEl.querySelector('input[type="checkbox"]') as HTMLInputElement | null;
  ok(runtimeToggle, "runtime toggle must exist");
  ok(runtimeToggle!.checked === false, "runtime toggle starts unchecked");
  await act(async () => {
    // Use click so React's controlled onChange sees the flipped checked state.
    runtimeToggle!.click();
    await flush();
  });
  await waitFor("runtime reload", () => calls.length >= 2 && calls[calls.length - 1] === true);

  const gotoBtn = Array.from(rootEl.querySelectorAll("button")).find((b) =>
    (b.textContent || "").includes("Open settings"),
  );
  ok(gotoBtn, "goto settings button must exist on issue with settings_tab");
  await act(async () => {
    gotoBtn!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await flush();
  });
  ok(navigations.includes("mcp"), `goto settings must navigate to mcp, got ${JSON.stringify(navigations)}`);

  const paths = rootEl.querySelectorAll(".diag-path");
  ok(paths.length > 0, "redacted paths must render");

  await act(async () => {
    root.unmount();
  });
}

console.log("diagnostics-settings: ok");
