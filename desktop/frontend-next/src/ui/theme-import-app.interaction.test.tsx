// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";
import { boot, STORAGE } from "../i18n";
import type { ThemeImport } from "../port/port";

beforeEach(async () => {
  await import("./Settings");
  sessionStorage.clear();
  localStorage.setItem(STORAGE, "zh");
  boot();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
});
afterEach(() => { cleanup(); sessionStorage.clear(); vi.restoreAllMocks(); });

it.each(["completed", "pending"])("isolates a %s remote theme import when the command palette opens a local session", async (state) => {
  const hub = new MockHub();
  const remote = await hub.openRemote({ host: "gpu-box", sessionPath: "/srv/sessions/pipeline.jsonl" });
  sessionStorage.setItem("rx-active-pane", remote.id);
  const build = hub.portFor.bind(hub);
  hub.portFor = (rt) => {
    const port = build(rt);
    port.providerSetup = async () => null;
    port.welcomeSeen = async () => true;
    port.themes = async () => [];
    return port;
  };
  const port = hub.portFor(remote);
  let finish!: (value: ThemeImport) => void;
  const imported = { pack: { id: "remote-only", name: "Remote only", tokens: {} } };
  vi.spyOn(port, "importTheme").mockImplementation(() => state === "completed"
    ? Promise.resolve(imported) : new Promise((resolve) => { finish = resolve; }));
  const activate = vi.spyOn(port, "activateTheme").mockResolvedValue();
  render(<App hub={hub} />);
  await screen.findByRole("combobox", { name: "任务输入" });
  await waitFor(() => expect(sessionStorage.getItem("rx-active-pane")).toBe(remote.id));
  await userEvent.click(screen.getByRole("button", { name: "沙盒" }));
  await userEvent.click(await screen.findByRole("tab", { name: /外观/ }));
  const sheet = document.querySelector(".prefs")!;
  fireEvent.change(sheet.querySelector('input[data-action="theme.import"]')!, {
    target: { files: [new File(["fixture"], "remote.zip")] },
  });
  if (state === "completed") await screen.findByRole("button", { name: "立即使用" });
  else await screen.findByRole("button", { name: "正在导入…" });
  await userEvent.keyboard("{Control>}k{/Control}");
  await userEvent.click(await screen.findByRole("button", { name: /^新建会话操作$/ }));
  await waitFor(() => expect(sessionStorage.getItem("rx-active-pane")).not.toBe(remote.id));
  expect(document.querySelector(".prefs")).toBe(sheet);
  if (state === "pending") await act(async () => finish(imported));
  expect(screen.queryByRole("button", { name: "立即使用" })).toBeNull();
  expect(sheet.textContent).not.toContain("Remote only");
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "导入主题…" }).disabled).toBe(false);
  expect(activate).not.toHaveBeenCalled();
});
