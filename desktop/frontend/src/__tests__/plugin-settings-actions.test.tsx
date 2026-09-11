import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { PluginsSettingsPage } from "../components/CapabilitiesPanel";
import type { AppBindings } from "../lib/bridge";
import { LocaleProvider } from "../lib/i18n";
import type { Meta, PluginInstallOptions, PluginView, TabMeta } from "../lib/types";
import { findButton, flush, installDom, setInputValue, waitFor } from "./capabilities-test-helpers";
import { installDesktopHostStub } from "./desktopHostStub";

function ok(value: unknown, message: string) {
  if (!value) throw new Error(message);
}

{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const meta: Meta = { label: "test", ready: true, eventChannel: "plugin-channel", cwd: "/tmp/reasonix-test", workspaceRoot: "/tmp/reasonix-test" };
  const tabs: TabMeta[] = [{
    id: "tab-plugin",
    scope: "project",
    workspaceRoot: "/tmp/reasonix-test",
    workspaceName: "reasonix-test",
    topicId: "topic-plugin",
    topicTitle: "Plugins",
    label: "Plugins",
    ready: true,
    running: false,
    mode: "normal",
    toolApprovalMode: "auto",
    active: true,
    cwd: "/tmp/reasonix-test",
  }];
  let planCalls = 0;
  let installCalls = 0;
  let toggleCalls = 0;
  let updateCalls = 0;
  let doctorCalls = 0;
  let removeCalls = 0;
  let pickFolderCalls = 0;
  const plannedSources: string[] = [];
  const installedSources: string[] = [];
  let plugins: PluginView[] = [{
    name: "superpowers",
    version: "0.1.0",
    description: "Shared agent skills and hooks.",
    source: "git:github.com/obra/superpowers",
    root: "~/.reasonix/plugins/superpowers",
    manifestKind: "reasonix",
    enabled: true,
    skills: 2,
    hooks: 1,
    mcpServers: 0,
  }];
  installDesktopHostStub(({
    main: {
      App: {
        Meta: async () => meta,
        ListTabs: async () => tabs,
        Plugins: async () => plugins.map((plugin) => ({ ...plugin, warnings: [...(plugin.warnings ?? [])] })),
        PlanPluginInstall: async (source: string, options: PluginInstallOptions) => {
          planCalls += 1;
          plannedSources.push(source);
          ok(options.dryRun === true, "plugin preview asks for dry-run planning");
          return JSON.stringify({
            ok: true,
            status: "planned",
            name: "superpowers",
            actions: [{
              kind: "plugin", action: "install_plugin_package", name: "superpowers", source, status: "planned",
              compatibility: "partial", mappedCapabilities: ["skills", "agents"],
              skippedCapabilities: [{ capability: "hook", path: "hooks/hooks.json", reason: "unsupported event" }],
            }],
          });
        },
        InstallPlugin: async (source: string, _options: PluginInstallOptions) => {
          installCalls += 1;
          installedSources.push(source);
          const next: PluginView = {
            name: "superpowers",
            version: "0.1.1",
            description: "Shared agent skills and hooks.",
            source,
            root: "~/.reasonix/plugins/superpowers",
            manifestKind: "reasonix",
            enabled: true,
            skills: 3,
            commands: 2,
            agents: 1,
            hooks: 1,
            mcpServers: 1,
            compatibility: "full",
            mappedCapabilities: ["skills", "agents", "hooks", "mcp"],
            skillDetails: [{ name: "plan", description: "Plan work before implementation.", invocation: "/superpowers:plan", runAs: "inline" }],
            agentDetails: [{ name: "reviewer", description: "Review changes.", invocation: "/superpowers:reviewer", model: "sonnet" }],
            commandDetails: [{
              name: "plan",
              description: "Plugin planning prompt.",
              invocation: "/superpowers:plan",
            }, {
              name: "blocked",
              description: "Occupied canonical command.",
              invocation: "/superpowers:blocked",
              shadowed: true,
            }],
            hookDetails: [{ event: "SessionStart", contextFile: "CLAUDE.md", description: "Load startup context." }],
            mcpServerDetails: [{ name: "context", displayName: "Context Search", transport: "stdio", command: "node server.js", autoStart: false }],
          };
          plugins = plugins.filter((plugin) => plugin.name !== next.name).concat(next);
          return JSON.stringify({ ok: true, status: "done", actions: [{ action: "install_plugin_package", name: next.name, status: "done" }] });
        },
        SetPluginEnabled: async (name: string, enabled: boolean) => {
          toggleCalls += 1;
          plugins = plugins.map((plugin) => plugin.name === name ? { ...plugin, enabled } : plugin);
        },
        UpdatePlugin: async (name: string) => {
          updateCalls += 1;
          plugins = plugins.map((plugin) => plugin.name === name ? { ...plugin, version: "0.1.2" } : plugin);
          return JSON.stringify({ ok: true, status: "done", name });
        },
        PluginDoctor: async (name: string) => {
          doctorCalls += 1;
          return { ...(plugins.find((plugin) => plugin.name === name) ?? plugins[0]), warnings: ["manifest exports no MCP auth metadata"] };
        },
        RemovePlugin: async (name: string) => {
          removeCalls += 1;
          plugins = plugins.filter((plugin) => plugin.name !== name);
        },
        PickPluginFolder: async () => {
          pickFolderCalls += 1;
          return "/tmp/superpowers-plugin";
        },
      } as Partial<AppBindings> as AppBindings,
    },
  }).main.App);

  await act(async () => {
    root.render(React.createElement(LocaleProvider, null, React.createElement(PluginsSettingsPage)));
    await flush();
  });
  await waitFor("superpowers plugin row", () => Boolean(document.querySelector(".cap-row__name")?.textContent?.includes("superpowers")));
  ok(document.querySelector<HTMLElement>("#settings-plugin-install")?.hidden === true, "plugin installer starts collapsed");
  const openInstaller = findButton("Install plugin");
  if (!openInstaller) throw new Error("missing plugin installer entry");
  await act(async () => {
    openInstaller.click();
    await flush();
  });
  ok(document.querySelector<HTMLElement>("#settings-plugin-install")?.hidden === false, "install entry reveals the plugin installer");
  ok(Boolean(document.querySelector(".cap-plugin-form-grid .cap-plugin-fields--local")), "local plugin install mode uses the shared form grid");
  const localOptionTexts = Array.from(document.querySelectorAll(".cap-plugin-installer__options > .cap-plugin-option-block"))
    .map((option) => option.textContent ?? "");
  ok(localOptionTexts[0]?.includes("Overwrite same-name plugin"), "local install mode shows overwrite before link mode");
  ok(localOptionTexts[1]?.includes("Developer mode: link source folder"), "local install mode shows link mode after overwrite");

  const chooseFolder = findButton("Choose plugin folder");
  if (!chooseFolder) throw new Error("missing plugin folder picker button");
  await act(async () => {
    chooseFolder.click();
    await flush();
  });
  await waitFor("picked plugin folder source", () => document.body.textContent?.includes("/tmp/superpowers-plugin") ?? false);
  ok(pickFolderCalls === 1, "clicking Choose folder invokes the plugin folder picker once");

  const gitMode = findButton("Git repository");
  if (!gitMode) throw new Error("missing Git repository install mode");
  await act(async () => {
    gitMode.click();
    await flush();
  });
  ok(Boolean(document.querySelector(".cap-plugin-form-grid .cap-plugin-fields--git")), "Git plugin install mode uses the shared form grid");
  const sourceInput = document.querySelector<HTMLInputElement>('input[aria-label="Git repository URL"]');
  if (!sourceInput) throw new Error("missing plugin git source input");
  await act(async () => {
    setInputValue(sourceInput, "git:github.com/obra/superpowers");
    await flush();
  });
  await waitFor("plugin preview enabled", () => findButton("Preview")?.disabled === false);

  const preview = findButton("Preview");
  if (!preview) throw new Error("missing plugin preview button");
  await act(async () => {
    preview.click();
    await flush();
  });
  await waitFor("plugin install plan", () => document.body.textContent?.includes("install_plugin_package") ?? false);
  ok(planCalls === 1, "clicking Preview invokes plugin install planning once");
  ok(plannedSources[0] === "git:github.com/obra/superpowers", "plugin preview receives the entered Git source");
  ok(document.body.textContent?.includes("Partially compatible") ?? false, "preview renders compatibility status");
  ok(document.body.textContent?.includes("Mapped: skills, agents") ?? false, "preview renders mapped capabilities");
  ok(document.body.textContent?.includes("hook: unsupported event") ?? false, "preview renders skipped capability reasons");

  const install = findButton("Install plugin");
  if (!install) throw new Error("missing plugin install button");
  await act(async () => {
    install.click();
    await flush();
  });
  await waitFor("plugin install result", () => installCalls === 1 && plugins[0]?.version === "0.1.1");
  ok(installedSources[0] === "git:github.com/obra/superpowers", "plugin install receives the entered Git source");

  const disclosure = document.querySelector<HTMLButtonElement>(".cap-plugin-entry .cap-disclosure");
  if (!disclosure) throw new Error("missing plugin disclosure");
  await act(async () => {
    disclosure.click();
    await flush();
  });
  await waitFor("plugin update action", () => Boolean(findButton("Update")));
  ok(document.body.textContent?.includes("How to use") ?? false, "expanded plugin details explain how to use the plugin");
  ok(document.body.textContent?.includes("/superpowers:plan") ?? false, "expanded plugin details list qualified skill invocations");
  ok(document.body.textContent?.includes("/superpowers:plan") ?? false, "plugin details show the canonical qualified invocation");
  ok(document.body.textContent?.includes("qualified name is occupied by a user or project command") ?? false, "occupied canonical command explains the winning source");
  ok(document.body.textContent?.includes("SessionStart") ?? false, "expanded plugin details list exported hooks");
  ok(document.body.textContent?.includes("Fully compatible") ?? false, "plugin details show structured compatibility");
  ok(document.body.textContent?.includes("/superpowers:reviewer") ?? false, "plugin details list imported agents");
  ok(document.body.textContent?.includes("Context Search") ?? false, "plugin details retain MCP display names");
  ok(document.body.textContent?.includes("on demand") ?? false, "imported MCP servers are labeled on demand");
  ok(document.body.textContent?.includes("context") ?? false, "expanded plugin details list exported MCP servers");

  const update = findButton("Update");
  if (!update) throw new Error("missing plugin update button");
  await act(async () => {
    update.click();
    await flush();
  });
  await waitFor("plugin update call", () => updateCalls === 1 && plugins[0]?.version === "0.1.2");

  const doctor = findButton("Doctor");
  if (!doctor) throw new Error("missing plugin doctor button");
  await act(async () => {
    doctor.click();
    await flush();
  });
  await waitFor("plugin diagnostic warning", () => document.body.textContent?.includes("manifest exports no MCP auth metadata") ?? false);
  ok(doctorCalls === 1, "clicking Doctor invokes plugin diagnostics once");

  const toggle = document.querySelector<HTMLInputElement>(".cap-plugin-entry .cap-switch input");
  if (!toggle) throw new Error("missing plugin enable toggle");
  await act(async () => {
    toggle.click();
    await flush();
  });
  await waitFor("plugin disabled", () => toggleCalls === 1 && plugins[0]?.enabled === false);

  const remove = findButton("Remove plugin");
  if (!remove) throw new Error("missing plugin remove button");
  await act(async () => {
    remove.click();
    await flush();
  });
  const confirmRemove = findButton("Confirm remove");
  if (!confirmRemove) throw new Error("missing plugin confirm remove button");
  await act(async () => {
    confirmRemove.click();
    await flush();
  });
  await waitFor("plugin removed", () => removeCalls === 1 && plugins.length === 0);

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log("plugin settings actions passed");
