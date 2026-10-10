// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { ServerRow } from "./ServerRow";
import { MockPort } from "../port/mock";
import type { AgentPort, McpEntry } from "../port/port";
import { boot, STORAGE, t } from "../i18n";

afterEach(() => {
  cleanup();
  localStorage.setItem(STORAGE, "zh");
  boot();
  vi.restoreAllMocks();
});

const cached: McpEntry = {
  name: "repository-tools", state: "failed", enabled: true, transport: "http",
  source: "project_config", tools: 2, remembered: true,
  toolList: [
    { name: "find_symbol", description: "Find a repository symbol", readOnly: true },
    { name: "rewrite_file", error: "unsupported input schema", destructive: true },
  ],
};
const note = (stale: boolean) => t(stale ? "上次连上时的记录 · 声明改过，可能对不上了" : "上次连接时的记录");

function draw(m: McpEntry, port: AgentPort, onDone = () => {}) {
  return <ServerRow m={m} port={port} onDone={onDone} root="/workspace" live />;
}

describe("a remembered MCP inventory", () => {
  it.each([
    ["zh", "failed", false], ["zh", "failed", true],
    ["en", "failed", false], ["en", "failed", true],
    ["zh", "standby", false], ["zh", "standby", true],
    ["en", "standby", false], ["en", "standby", true],
    ["zh", "disabled", false], ["zh", "disabled", true],
    ["en", "disabled", false], ["en", "disabled", true],
  ] as const)("shows %s %s cache provenance (stale=%s) without a server description", async (lang, state, stale) => {
    localStorage.setItem(STORAGE, lang);
    boot();
    const port = new MockPort() as unknown as AgentPort;
    const reconnect = vi.spyOn(port, "reconnectMcp");
    const view = render(draw({ ...cached, state, stale, enabled: state !== "disabled" }, port));
    await userEvent.click(view.container.querySelector("summary")!);
    const badge = screen.getByText(note(stale));
    expect(badge.getAttribute("title")).toBe(t("当前未连接，以下是上次连接时它返回的内容。"));
    expect(screen.getByText("find_symbol")).toBeTruthy();
    expect(screen.getByText("rewrite_file")).toBeTruthy();
    expect(screen.getByText("unsupported input schema")).toBeTruthy();
    expect(screen.getByText(t("只读"))).toBeTruthy();
    expect(screen.getByText(t("会修改数据"))).toBeTruthy();
    expect(screen.getByText(t("{n} 个工具", { n: 2 }))).toBeTruthy();
    expect(reconnect).not.toHaveBeenCalled();
  });

  it.each([false, true])("keeps cached self-description and the matching stale=%s notice together", (stale) => {
    render(draw({ ...cached, stale, description: "Repository indexing service" }, new MockPort() as unknown as AgentPort));
    expect(screen.getByText("Repository indexing service")).toBeTruthy();
    expect(screen.getByText(note(stale))).toBeTruthy();
  });

  it.each([undefined, "Repository indexing service"])("does not mark a live server as cached (description=%j)", (description) => {
    render(draw({ ...cached, state: "ready", remembered: false, description }, new MockPort() as unknown as AgentPort));
    expect(screen.queryByText(note(false))).toBeNull();
    expect(screen.queryByText(note(true))).toBeNull();
    expect(screen.queryByRole("button", { name: "重连" })).toBeNull();
    expect(screen.getByText("find_symbol")).toBeTruthy();
  });

  it("keeps the cache notice while reconnecting and follows the refreshed inventory", async () => {
    const port = new MockPort() as unknown as AgentPort;
    let finish!: () => void;
    const reconnect = vi.spyOn(port, "reconnectMcp").mockImplementationOnce(() => new Promise((resolve) => {
      finish = () => resolve({ state: "ready" });
    }));
    const onDone = vi.fn();
    const view = render(draw(cached, port, onDone));
    await userEvent.click(screen.getByRole("button", { name: "重连" }));
    expect(screen.getByText(note(false))).toBeTruthy();
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "连接中…" }).disabled).toBe(true);
    await act(async () => finish());
    expect(reconnect).toHaveBeenCalledExactlyOnceWith("repository-tools");
    expect(onDone).toHaveBeenCalledTimes(1);
    view.rerender(draw({ ...cached, state: "ready", remembered: false, tools: 1, toolList: cached.toolList!.slice(0, 1) }, port, onDone));
    expect(screen.queryByText(note(false))).toBeNull();
    expect(screen.queryByText("rewrite_file")).toBeNull();
    expect(screen.getByText("find_symbol")).toBeTruthy();
    view.rerender(draw({ ...cached, stale: true, error: "connection offline" }, port, onDone));
    expect(screen.getByText(note(true))).toBeTruthy();
    expect(screen.getByText("connection offline")).toBeTruthy();
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "重连" }).disabled).toBe(false);
  });
});
