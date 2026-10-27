// @vitest-environment jsdom
import "./testkit";
import { act, cleanup, render, renderHook, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { AgentPresets } from "./AgentPresets";
import { useOpenPane } from "./useOpenPane";
import { useOpenRemotePane } from "./useOpenRemotePane";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import { HttpError } from "../port/http_error";
import { NEW_TASK_PRESET_KEY, newTaskPreset, setNewTaskPreset } from "../state/newTaskPreset";

afterEach(() => { cleanup(); vi.restoreAllMocks(); localStorage.removeItem(NEW_TASK_PRESET_KEY); });

const sw = () => ({ path: "", pending: false, opening: () => false, begin: () => 1,
  current: () => true, landed: vi.fn(), end: vi.fn(), cancel: vi.fn() });

it("uses the clicked card for fresh local and remote tasks, with explicit choices and history preserved", async () => {
  const hub = new MockHub();
  const localOpen = vi.spyOn(hub, "open");
  const remoteOpen = vi.spyOn(hub, "openRemote");
  const port = new MockPort();
  await port.setDefaultAgentPreset("builtin:minimal", false);
  render(<AgentPresets port={port} />);
  const user = userEvent.setup();
  const general = await screen.findByRole("button", { name: "将日常搭档设为新任务默认" });
  expect(general.getAttribute("aria-pressed")).toBe("true");
  expect(newTaskPreset()).toBeUndefined();
  await user.click(screen.getByRole("button", { name: "将轻装搭档设为新任务默认" }));
  expect(screen.getByRole("button", { name: "将轻装搭档设为新任务默认" }).getAttribute("aria-pressed")).toBe("true");
  expect(await port.agentComposition()).toBeNull();
  const local = renderHook(() => useOpenPane({ hub, runtimes: [], panePorts: new Map(), active: "",
    reportsRef: { current: {} }, runsRef: { current: {} }, setRuntimes: vi.fn(), setActive: vi.fn(),
    setTakeover: vi.fn(), reloadPanes: async () => {}, sw: sw() }));
  await act(async () => { await local.result.current({ root: "/task" }); });
  expect(localOpen).toHaveBeenLastCalledWith({ root: "/task", agentComposition: "builtin:minimal" });
  const created = (await hub.runtimes()).find(v => v.root === "/task")!;
  expect((await hub.portFor(created).agentComposition())?.id).toBe("builtin:minimal");
  const remote = renderHook(() => useOpenRemotePane({ hub, reloadPanes: async () => {},
    reloadRemotes: async () => {}, sw: sw(), setActive: vi.fn(), setOpening: vi.fn() }));
  await act(async () => { await remote.result.current("gpu-box", "/task"); });
  expect(remoteOpen).toHaveBeenLastCalledWith({ host: "gpu-box", workspace: "/task", sessionPath: undefined, agentComposition: "builtin:minimal" });
  const remoteCreated = (await hub.runtimes()).find(v => v.host === "gpu-box")!;
  expect((await hub.portFor(remoteCreated).agentComposition())?.id).toBe("builtin:minimal");
  await act(async () => { await local.result.current({ root: "/task", agentComposition: "builtin:general" }); });
  expect(localOpen).toHaveBeenLastCalledWith({ root: "/task", agentComposition: "builtin:general" });
  await user.click(screen.getByRole("button", { name: "将日常搭档设为新任务默认" }));
  await act(async () => { await local.result.current({ root: "/reset" }); });
  const reset = (await hub.runtimes()).find(v => v.root === "/reset")!;
  expect(await hub.portFor(reset).agentComposition()).toBeNull();
  expect((await hub.portFor(created).agentComposition())?.id).toBe("builtin:minimal");
  await act(async () => { await local.result.current({ root: "/task", sessionPath: "/history.jsonl" }); });
  expect(localOpen).toHaveBeenLastCalledWith({ root: "/task", sessionPath: "/history.jsonl" });
  await act(async () => { await remote.result.current("gpu-box", "/task", "/history.jsonl"); });
  expect(remoteOpen).toHaveBeenLastCalledWith({ host: "gpu-box", workspace: "/task", sessionPath: "/history.jsonl", agentComposition: undefined });
});

it("keeps usage, details and copy actions separate from selecting the new task default", async () => {
  const user = userEvent.setup();
  setNewTaskPreset("builtin:general");
  render(<AgentPresets port={new MockPort()} />);
  const usage = await screen.findByRole("button", { name: "如何使用 轻装搭档" });
  await user.click(usage);
  expect(usage.getAttribute("aria-expanded")).toBe("true");
  expect(screen.getByText(/在新任务输入框选择轻装搭档/)).toBeTruthy();
  expect(newTaskPreset()).toBe("builtin:general");
  await user.click(screen.getByRole("button", { name: "查看 轻装搭档" }));
  await screen.findByRole("form", { name: "预设详情" });
  expect(newTaskPreset()).toBe("builtin:general");
  await user.click(screen.getByRole("button", { name: "返回预设列表" }));
  await user.click(screen.getByRole("button", { name: "复制 轻装搭档" }));
  expect(await screen.findByRole("form", { name: "新建预设" })).toBeTruthy();
  expect(newTaskPreset()).toBe("builtin:general");
});

it("keeps the old default badge and reports a storage refusal", async () => {
  const user = userEvent.setup();
  render(<AgentPresets port={new MockPort()} />);
  await screen.findByRole("button", { name: "将轻装搭档设为新任务默认" });
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("storage denied"); });
  await user.click(screen.getByRole("button", { name: "将轻装搭档设为新任务默认" }));
  expect(await screen.findByRole("alert")).toBeTruthy();
  expect(screen.getByRole("button", { name: "将日常搭档设为新任务默认" }).getAttribute("aria-pressed")).toBe("true");
});

it("lets a personal preset be the new task default, marked on its own row", async () => {
  const port = new MockPort();
  const base = (await port.agentPresets()).presets.find(v => v.definition.id === "builtin:general")!.definition;
  await port.saveAgentPreset({ ...base, id: "user:daily-pro", name: "日常搭档pro" }, "");
  render(<AgentPresets port={port} />);
  const user = userEvent.setup();
  const pick = await screen.findByRole("button", { name: "将日常搭档pro设为新任务默认" });
  expect(pick.getAttribute("aria-pressed")).toBe("false");
  await user.click(pick);
  expect(newTaskPreset()).toBe("user:daily-pro");
  expect(screen.getByRole("button", { name: "将日常搭档pro设为新任务默认" }).getAttribute("aria-pressed")).toBe("true");
  expect(screen.getByRole("button", { name: "将日常搭档设为新任务默认" }).getAttribute("aria-pressed")).toBe("false");
});

it("forgets a personal default that no longer exists and opens the task with the built-in one", async () => {
  const hub = new MockHub();
  const open = vi.spyOn(hub, "open")
    .mockRejectedValueOnce(new HttpError(404, "no such preset", { code: "agent_composition.not_found" }));
  setNewTaskPreset("user:deleted");
  const local = renderHook(() => useOpenPane({ hub, runtimes: [], panePorts: new Map(), active: "",
    reportsRef: { current: {} }, runsRef: { current: {} }, setRuntimes: vi.fn(), setActive: vi.fn(),
    setTakeover: vi.fn(), reloadPanes: async () => {}, sw: sw() }));
  await act(async () => { await local.result.current({ root: "/task" }); });
  expect(open).toHaveBeenNthCalledWith(1, { root: "/task", agentComposition: "user:deleted" });
  expect(open).toHaveBeenNthCalledWith(2, { root: "/task" });
  expect(newTaskPreset()).toBeUndefined();
});

it("does not hide a refusal the person asked for by name", async () => {
  const hub = new MockHub();
  vi.spyOn(hub, "open").mockRejectedValue(new HttpError(404, "no such preset", { code: "agent_composition.not_found" }));
  setNewTaskPreset("user:kept");
  const local = renderHook(() => useOpenPane({ hub, runtimes: [], panePorts: new Map(), active: "",
    reportsRef: { current: {} }, runsRef: { current: {} }, setRuntimes: vi.fn(), setActive: vi.fn(),
    setTakeover: vi.fn(), reloadPanes: async () => {}, sw: sw() }));
  await expect(act(async () => { await local.result.current({ root: "/task", agentComposition: "user:asked" }); })).rejects.toBeTruthy();
  expect(newTaskPreset()).toBe("user:kept");
});
