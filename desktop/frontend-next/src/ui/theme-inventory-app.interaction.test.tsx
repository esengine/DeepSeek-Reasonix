// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";
import { boot, STORAGE } from "../i18n";
import type { ThemePack } from "../port/port";

beforeEach(async () => {
  await import("./Settings");
  sessionStorage.clear();
  localStorage.setItem(STORAGE, "zh");
  boot();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
});
afterEach(() => { cleanup(); sessionStorage.clear(); vi.restoreAllMocks(); });

it.each(["completed", "late success", "late failure"])("isolates a remote theme inventory with %s when the palette opens a local session", async (state) => {
  const hub = new MockHub();
  const remote = await hub.openRemote({ host: "gpu-box", sessionPath: "/srv/sessions/pipeline.jsonl" });
  sessionStorage.setItem("rx-active-pane", remote.id);
  let finishLocal!: (packs: ThemePack[]) => void;
  const local = new Promise<ThemePack[]>((resolve) => { finishLocal = resolve; });
  const build = hub.portFor.bind(hub);
  const prepared = new WeakSet();
  hub.portFor = (rt) => {
    const port = build(rt);
    if (!prepared.has(port)) {
      prepared.add(port);
      port.providerSetup = async () => null;
      port.welcomeSeen = async () => true;
      port.themes = () => local;
    }
    return port;
  };
  const connection = hub.portFor(remote);
  let finish!: (packs: ThemePack[]) => void;
  let fail!: (error: Error) => void;
  const packs = [{ id: "remote", name: "Remote pack", tokens: {} }];
  vi.spyOn(connection, "themes").mockImplementation(() => state === "completed"
    ? Promise.resolve(packs) : new Promise((resolve, reject) => { finish = resolve; fail = reject; }));
  render(<App hub={hub} />);
  await screen.findByRole("combobox", { name: "任务输入" });
  await waitFor(() => expect(sessionStorage.getItem("rx-active-pane")).toBe(remote.id));
  await userEvent.click(screen.getByRole("button", { name: "沙盒" }));
  await userEvent.click(await screen.findByRole("tab", { name: /外观/ }));
  const sheet = document.querySelector(".prefs")!;
  if (state === "completed") await screen.findByRole("button", { name: /Remote pack/ });
  await userEvent.keyboard("{Control>}k{/Control}");
  await userEvent.click(await screen.findByRole("button", { name: /^新建会话操作$/ }));
  await waitFor(() => expect(sessionStorage.getItem("rx-active-pane")).not.toBe(remote.id));
  expect(document.querySelector(".prefs")).toBe(sheet);
  if (state !== "completed") await act(async () => state === "late success" ? finish(packs) : fail(new Error("Remote read failed")));
  expect(screen.queryByRole("button", { name: /Remote pack/ })).toBeNull();
  expect(screen.queryByText("Remote read failed")).toBeNull();
  await act(async () => finishLocal([{ id: "local", name: "Local pack", tokens: {} }]));
  expect(screen.getByRole("button", { name: /Local pack/ })).toBeTruthy();
});
