// @vitest-environment jsdom
import { StrictMode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import { boot, STORAGE, t } from "../i18n";
import type { AgentPort, PluginHook, PluginPackage, SessionStatus } from "../port/port";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

const tail: PluginHook = { event: "Stop", command: "echo tail", description: "Tail hook" };
const head: PluginHook = { event: "PreToolUse", command: "echo head", description: "Head hook" };
const duplicates: Record<string, PluginHook[]> = {
  context: [
    { event: "SessionStart", contextFile: "context/first.md", description: "First context" },
    { event: "SessionStart", contextFile: "context/second.md", description: "Second context" },
  ],
  command: [
    { event: "SessionStart", command: "echo repeated", description: "First command" },
    { event: "SessionStart", command: "echo repeated", description: "Second command" },
  ],
};

function draw(port: AgentPort, onChanged: () => void, strict: boolean) {
  const settings = <Settings
    hub={new MockHub() as never} port={port}
    status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{} as never} onLook={() => {}} reloadThemes={() => {}}
    onClose={() => {}} onChanged={onChanged} onError={() => {}} at="ext:installed"
    account={null} accountUnread="" reloadAccount={() => {}}
  />;
  return strict ? <StrictMode>{settings}</StrictMode> : settings;
}

it.each(["context", "command"].flatMap((kind) =>
  ["remove", "prepend"].flatMap((change) => [false, true].map((strict) => ({ kind, change, strict }))),
))("refreshes repeated $kind hook rows after $change (StrictMode=$strict)", async ({ kind, change, strict }) => {
  const port = new MockPort() as unknown as AgentPort;
  const original = [...duplicates[kind], tail];
  let hooks = original;
  const pkg: PluginPackage = {
    name: "hook-kit", root: "/plugins/hook-kit", source: "/sources/hook-kit", enabled: true,
    runtime: { command: "persistent-runtime", tools: ["persistent-tool"] },
    mcpServers: [{ name: "persistent-server", description: "Persistent server" }],
    skills: [{ name: "greet", invocation: "/hook-kit:greet", description: "Persistent skill" }],
  };
  vi.spyOn(port, "plugins").mockImplementation(async () => [{ ...pkg, hooks }]);
  const reload = vi.spyOn(port, "reloadExtensions").mockResolvedValue();
  const changed = vi.fn();
  render(draw(port, changed, strict));
  await screen.findByText("hook-kit");
  const element = document.querySelector<HTMLElement>('[data-extension-name="hook-kit"]')!;
  const pane = within(element);
  const rows = () => Array.from(element.querySelectorAll(".peek [data-run] .sc"), (row) => row.textContent);
  const expectRows = (current: string[]) => {
    expect(rows()).toEqual(["persistent-runtime", "persistent-tool", ...current, "Persistent server"]);
    expect(pane.getAllByText("/hook-kit:greet")).toHaveLength(1);
    expect(pane.getAllByText("Persistent skill")).toHaveLength(1);
  };
  expectRows([`First ${kind}`, `Second ${kind}`, "Tail hook"]);
  await userEvent.click(pane.getByRole("button", { name: t("移除 {name}", { name: pkg.name }) }));
  expect(pane.getByRole("button", { name: t("取消") })).toBeTruthy();

  const refresh = async (next: PluginHook[], expected: string[]) => {
    hooks = next;
    await userEvent.click(document.querySelector<HTMLButtonElement>('[data-action="extensions.reload"]')!);
    await waitFor(() => expectRows(expected));
    expect(document.querySelector('[data-extension-name="hook-kit"]')).toBe(element);
    expect(pane.getByRole("button", { name: t("取消") })).toBeTruthy();
    expect(pane.getByRole<HTMLButtonElement>("button", { name: t("导出") }).disabled).toBe(false);
    expect(pane.getByRole<HTMLButtonElement>("switch").disabled).toBe(false);
  };
  if (change === "remove") await refresh([tail], ["Tail hook"]);
  else await refresh([head, ...original], ["Head hook", `First ${kind}`, `Second ${kind}`, "Tail hook"]);
  await refresh(original, [`First ${kind}`, `Second ${kind}`, "Tail hook"]);
  await refresh([tail], ["Tail hook"]);
  await refresh([], []);
  expect(reload).toHaveBeenCalledTimes(4);
  expect(changed).toHaveBeenCalledTimes(4);
});
