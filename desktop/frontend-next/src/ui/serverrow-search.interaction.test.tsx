// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { ServerRow } from "./ServerRow";
import { MockPort } from "../port/mock";
import { boot } from "../i18n";
import type { AgentPort, McpEntry } from "../port/port";

beforeEach(() => { localStorage.setItem("rx-lang", "zh"); boot(); });
afterEach(() => { cleanup(); localStorage.setItem("rx-lang", "zh"); boot(); vi.restoreAllMocks(); });

const server: McpEntry = {
  name: "docs", description: "Reference documentation", enabled: true, state: "ready", tools: 6, source: "project_config",
  toolList: [
    { name: "read_notes", description: "Read project notes", readOnly: true },
    { name: "write_notes", description: "Update project notes", destructive: true },
    { name: "café_lookup", description: "查找资料" },
    { name: "inspect[raw]", description: "Inspect supplied schema", error: "Unsupported schema keyword" },
    { name: "health_status", description: "Service health" },
    { name: "archive_records", description: "Saved records" },
  ],
};

const port = () => new MockPort() as unknown as AgentPort;
const row = (name = "docs") => within(screen.getByText(name).closest<HTMLElement>(".srv")!);
const search = (name = "docs") => row(name).getByRole<HTMLInputElement>("searchbox", { name: `搜索 ${name} 的工具` });
async function open(name = "docs") { await userEvent.click(screen.getByText(name).closest("summary")!); }

it.each([
  ["READ_NOTES", ["read_notes"]],
  ["update", ["write_notes"]],
  ["notes read", ["read_notes"]],
  ["  project\tNOTES  ", ["read_notes", "write_notes"]],
  ["cafe\u0301", ["café_lookup"]],
  ["查找", ["café_lookup"]],
  ["schema unsupported", ["inspect[raw]"]],
  ["inspect[raw]", ["inspect[raw]"]],
  ["   ", ["read_notes", "write_notes", "café_lookup", "inspect[raw]", "health_status", "archive_records"]],
  ["read unavailable", []],
] as const)("finds tools from supplied metadata for %s", async (query, names) => {
  const p = port();
  const reconnect = vi.spyOn(p, "reconnectMcp");
  const load = vi.spyOn(p, "setMcpLoad");
  const done = vi.fn();
  const { container } = render(<ServerRow m={server} port={p} root="/w" live onDone={done} />);
  await open();
  await userEvent.type(search(), query.replaceAll("[", "[["));
  expect([...container.querySelectorAll(".trow .nm")].map((el) => el.textContent)).toEqual(names);
  expect(row().getByText("6 个工具")).toBeTruthy();
  expect(reconnect).not.toHaveBeenCalled();
  expect(load).not.toHaveBeenCalled();
  expect(done).not.toHaveBeenCalled();
});

it("clears an empty result and returns keyboard focus to search", async () => {
  render(<ServerRow m={server} port={port()} root="/w" live onDone={() => {}} />);
  await open();
  await userEvent.type(search(), "missing");
  expect(row().getByRole("status").textContent).toBe("没有匹配的工具。");
  await userEvent.click(row().getByRole("button", { name: "清除" }));
  expect(search().value).toBe("");
  expect(document.activeElement).toBe(search());
  expect(row().getByText("read_notes")).toBeTruthy();
  expect(row().queryByRole("status")).toBeNull();
});

it("keeps each server's query separate and retains it across folding", async () => {
  const p = port();
  render(<><ServerRow m={server} port={p} root="/w" live onDone={() => {}} />
    <ServerRow m={{ ...server, name: "other" }} port={p} root="/w" live onDone={() => {}} /></>);
  await open(); await open("other");
  await userEvent.type(search(), "write");
  expect(search("other").value).toBe("");
  expect(row("other").getByText("read_notes")).toBeTruthy();
  await open(); await open();
  expect(search().value).toBe("write");
  expect(row().queryByText("read_notes")).toBeNull();
});

it.each(["port", "root", "return-port", "return-root"])("resets search for a new %s owner", async (change) => {
  const p = port(), next = port();
  const draw = (active: AgentPort, root: string) => <ServerRow m={server} port={active} root={root} live onDone={() => {}} />;
  const view = render(draw(p, "/w"));
  await open(); await userEvent.type(search(), "write");
  const connection = change.includes("port");
  view.rerender(draw(connection ? next : p, connection ? "/w" : "/other"));
  if (change.startsWith("return")) view.rerender(draw(p, "/w"));
  expect(search().value).toBe("");
  expect(row().getByText("read_notes")).toBeTruthy();
});

it("filters a refreshed inventory without replacing its query or remembered warning", async () => {
  const p = port();
  const draw = (m: McpEntry) => <ServerRow m={m} port={p} root="/w" live={false} onDone={() => {}} />;
  const view = render(draw({ ...server, state: "idle", remembered: true, stale: true }));
  await open(); await userEvent.type(search(), "missing");
  expect(row().getByRole("status")).toBeTruthy();
  view.rerender(draw({ ...server, state: "idle", remembered: true, stale: true,
    tools: 2, toolList: [{ name: "archive", description: "Read saved notes", readOnly: true }, { name: "health", description: "Service health" }] }));
  expect(row().queryByRole("searchbox")).toBeNull();
  expect(row().queryByRole("status")).toBeNull();
  expect(row().getByText("archive")).toBeTruthy();
  expect(row().getByText("health")).toBeTruthy();
  expect(row().getByText("上次连上时的记录 · 声明改过，可能对不上了")).toBeTruthy();
  expect(row().getByText("只读")).toBeTruthy();
  expect(row().queryByRole("button", { name: "常驻" })).toBeNull();
});

it.each(["success", "failure"])("keeps search independent while reconnect %s settles", async (outcome) => {
  const p = port(); let finish!: () => void; let fail!: (error: Error) => void;
  const pending = new Promise<void>((resolve, reject) => { finish = resolve; fail = reject; });
  const reconnect = vi.spyOn(p, "reconnectMcp").mockImplementation(() => pending.then(() => ({ state: "ready" })));
  const onDone = vi.fn();
  render(<ServerRow m={{ ...server, state: "failed", error: "Connection offline" }} port={p} root="/w" live onDone={onDone} />);
  await open(); await userEvent.click(row().getByRole("button", { name: "重连" }));
  await userEvent.type(search(), "missing");
  expect(row().getByText("Connection offline")).toBeTruthy();
  expect(row().getByRole<HTMLButtonElement>("button", { name: "连接中…" }).disabled).toBe(true);
  await act(async () => outcome === "success" ? finish() : fail(new Error("Retry unavailable")));
  expect(search().value).toBe("missing");
  expect(row().getByRole<HTMLButtonElement>("button", { name: "重连" }).disabled).toBe(false);
  expect(row().getByText(outcome === "failure" ? "Retry unavailable" : "Connection offline")).toBeTruthy();
  expect(reconnect).toHaveBeenCalledTimes(1); expect(onDone).toHaveBeenCalledTimes(1);
});

it("preserves removal confirmation and annotation truth when all tools are filtered away", async () => {
  render(<ServerRow m={server} port={port()} root="/w" live onDone={() => {}} />);
  await open(); await userEvent.type(search(), "missing");
  await userEvent.click(row().getByRole("button", { name: "移除 docs" }));
  expect(row().getByText(/从 project_config 中删除 docs/)).toBeTruthy();
  await userEvent.click(row().getByRole("button", { name: "清除" }));
  expect(row().getByText("会修改数据")).toBeTruthy();
  expect(row().getByText("Unsupported schema keyword").closest(".trow")?.hasAttribute("data-bad")).toBe(true);
  await userEvent.click(row().getByRole("button", { name: "取消" }));
  expect(row().queryByText(/从 project_config 中删除 docs/)).toBeNull();
});

it("labels search and its empty result in English", async () => {
  localStorage.setItem("rx-lang", "en"); boot();
  render(<ServerRow m={server} port={port()} root="/w" live onDone={() => {}} />);
  await open();
  await userEvent.type(row().getByRole("searchbox", { name: "Search tools in docs" }), "missing");
  expect(row().getByRole("status").textContent).toBe("No matching tool.");
  await userEvent.click(row().getByRole("button", { name: "Clear" }));
  expect(row().getByText("read_notes")).toBeTruthy();
});

it.each([undefined, []])("does not claim a failed search for an absent or empty inventory %s", (toolList) => {
  render(<ServerRow m={{ ...server, tools: 0, toolList }} port={port()} root="/w" live onDone={() => {}} />);
  expect(row().queryByRole("searchbox")).toBeNull();
  expect(row().queryByRole("status")).toBeNull();
});

it.each([1, 5, 6])("offers search only above five supplied tools: %s", async (count) => {
  const inventory = server.toolList!.slice(0, count);
  render(<ServerRow m={{ ...server, tools: count, toolList: inventory }} port={port()} root="/w" live onDone={() => {}} />);
  await open();
  expect(row().queryByRole("searchbox") !== null).toBe(count > 5);
  expect(row().queryByRole("status")).toBeNull();
  for (const tool of inventory) expect(row().getByText(tool.name)).toBeTruthy();
});
