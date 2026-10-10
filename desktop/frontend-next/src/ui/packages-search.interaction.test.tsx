// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Packages } from "./Packages";
import { MockPort } from "../port/mock";
import { boot, STORAGE, t } from "../i18n";
import type { AgentPort, PluginExport, PluginPackage } from "../port/port";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

async function draw(lang = "zh") {
  localStorage.setItem(STORAGE, lang);
  boot();
  const port = new MockPort() as unknown as AgentPort;
  const packages = await port.plugins();
  const props = { port, packages, onChanged: vi.fn(), updating: "", onUpdate: vi.fn() };
  const view = render(<Packages {...props} />);
  return { props, view };
}

const search = () => screen.getByRole<HTMLInputElement>("searchbox", { name: t("搜索已安装包") });
const row = (name = "review-kit") => document.querySelector<HTMLDetailsElement>(`[data-extension-name="${name}"]`)!;

it.each(["zh", "en"])("finds a package by capability, clears with the keyboard and keeps focus (%s)", async (lang) => {
  const { props } = await draw(lang);
  const read = vi.spyOn(props.port, "plugins");
  const toggle = vi.spyOn(props.port, "setPluginEnabled");
  await userEvent.tab();
  expect(document.activeElement).toBe(search());
  await userEvent.type(search(), "  /REVIEW-KIT:PR  ");
  expect(row().hidden).toBe(false);
  expect(row("notion-bridge").hidden).toBe(true);
  expect(screen.getByText(t("插件包：{shown} / {total}", { shown: 1, total: 2 }))).toBeTruthy();
  await userEvent.clear(search());
  await userEvent.type(search(), "nothing-here");
  expect(screen.getByText(t("没有匹配的已安装包。"))).toBeTruthy();
  expect(screen.queryByRole("switch")).toBeNull();
  await userEvent.tab();
  expect(document.activeElement).toBe(screen.getByRole("button", { name: t("清除搜索") }));
  await userEvent.keyboard("{Enter}");
  expect(search().value).toBe("");
  expect(document.activeElement).toBe(search());
  expect(screen.getAllByRole("switch")).toHaveLength(2);
  expect(read).not.toHaveBeenCalled();
  expect(toggle).not.toHaveBeenCalled();
  expect(props.onUpdate).not.toHaveBeenCalled();
});

it.each([
  ["review-kit", ["review-kit", "risk", "按改动范围逐块评审", "pr", "/review-kit:pr", "audit-agent", "Review specialist", "outline", "Write a plan", "midnight", "Dark theme"]],
  ["notion-bridge", ["SessionStart", "开会话时同步一次", "hooks/sync.sh", "records", "Workspace bridge", "Search pages", "notion-mcp", "bin/bridge", "--serve", "lookup-record"]],
].flatMap(([owner, terms]) => (terms as string[]).map((query) => [owner as string, query])))("finds %s by %s without changing its identity", async (ownerName, query) => {
  const { props, view } = await draw();
  const packages: PluginPackage[] = props.packages.map((p) => p.name === "review-kit" ? {
    ...p,
    agents: [{ name: "audit-agent", description: "Review specialist" }],
    prompts: [{ name: "outline", description: "Write a plan" }],
    themes: [{ name: "midnight", description: "Dark theme" }],
  } : {
    ...p,
    mcpServers: [{ name: "records", displayName: "Workspace bridge", description: "Search pages", command: "notion-mcp" }],
    runtime: { command: "bin/bridge", args: ["--serve"], tools: ["lookup-record"] },
  });
  view.rerender(<Packages {...props} packages={packages} />);
  await userEvent.type(search(), query);
  const owner = packages.find((p) => p.name === ownerName)!;
  expect(screen.getByRole("switch", { name: `${t(owner.enabled ? "关闭" : "启用")} ${owner.name}` })).toBeTruthy();
  expect(screen.getAllByRole("switch")).toHaveLength(1);
});

it("keeps a remove confirmation and manually opened inventory across filtering and refresh", async () => {
  const { props, view } = await draw();
  const original = row();
  await userEvent.click(within(original).getByRole("button", { name: "移除 review-kit" }));
  await userEvent.type(search(), "notion");
  expect(original.hidden).toBe(true);
  view.rerender(<Packages {...props} packages={props.packages.map((p) => ({ ...p }))} />);
  await userEvent.click(screen.getByRole("button", { name: "清除搜索" }));
  expect(row()).toBe(original);
  expect(within(row()).getByRole("button", { name: "删除" })).toBeTruthy();
  await userEvent.click(within(row()).getByRole("button", { name: "取消" }));
  await userEvent.click(row().querySelector("summary")!);
  expect(row().open).toBe(true);
  await userEvent.type(search(), "notion");
  await userEvent.click(screen.getByRole("button", { name: "清除搜索" }));
  expect(row().open).toBe(true);
});

it.each(["success", "failure"])("keeps a pending export and its %s result while hidden", async (outcome) => {
  const { props } = await draw();
  let finish!: (out: PluginExport) => void;
  let fail!: (error: Error) => void;
  const exportCall = vi.spyOn(props.port, "exportPlugin").mockImplementationOnce(() => new Promise((resolve, reject) => { finish = resolve; fail = reject; }));
  await userEvent.click(within(row()).getByRole("button", { name: "导出" }));
  await userEvent.type(search(), "notion");
  await act(async () => outcome === "success" ? finish({ savedTo: "/tmp/search-export.zip", required: [] }) : fail(new Error("export unavailable")));
  expect(props.onChanged).toHaveBeenCalledTimes(1);
  await userEvent.click(screen.getByRole("button", { name: "清除搜索" }));
  expect(screen.getByText(outcome === "success" ? /search-export.zip/ : "export unavailable")).toBeTruthy();
  expect(within(row()).getByRole<HTMLButtonElement>("button", { name: "导出" }).disabled).toBe(false);
  expect(exportCall).toHaveBeenCalledTimes(1);
});

it("uses refreshed inventory and clears old operation state on a new connection", async () => {
  const { props, view } = await draw();
  await userEvent.click(within(row()).getByRole("button", { name: "移除 review-kit" }));
  await userEvent.type(search(), "new-command");
  expect(screen.queryByRole("switch")).toBeNull();
  const packages = props.packages.map((p) => p.name === "review-kit" ? { ...p, commands: [{ name: "new-command", invocation: "/review-kit:new-command" }] } : p);
  view.rerender(<Packages {...props} packages={packages} />);
  expect(within(row()).getByRole("button", { name: "删除" })).toBeTruthy();
  view.rerender(<Packages {...props} packages={packages} port={new MockPort() as unknown as AgentPort} />);
  expect(search().value).toBe("new-command");
  expect(within(row()).queryByRole("button", { name: "删除" })).toBeNull();
  expect(screen.getByRole("switch", { name: "关闭 review-kit" })).toBeTruthy();
});
