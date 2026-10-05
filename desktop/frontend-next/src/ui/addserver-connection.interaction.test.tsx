// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { AddServer } from "./AddServer";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { AgentPort, McpDraft, McpInstallResult, SessionStatus } from "../port/port";

afterEach(cleanup);

const oldDraft: McpDraft = { servers: [{ name: "old-docs", transport: "http", url: "https://example.test/old" }], risks: [] };
const newDraft: McpDraft = { servers: [{ name: "new-docs", transport: "http", url: "https://example.test/new" }], risks: [] };
const failure = "previous MCP request failed";
const result = (name: string): McpInstallResult => ({ name, state: "ready", toolCount: 1, action: "installed", message: "" });

function deferred<T>() {
  let finish!: (value: T) => void;
  let fail!: (error: Error) => void;
  const promise = new Promise<T>((resolve, reject) => { finish = resolve; fail = reject; });
  return { promise, finish, fail: () => fail(new Error(failure)) };
}

function settings(port: AgentPort, onChanged = vi.fn()) {
  return <Settings
    hub={new MockHub() as never} port={port}
    status={{ preset: "balanced", toolApprovalMode: "ask", workspaceRoot: "/workspace/main" } as SessionStatus}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{} as never} onLook={() => {}} reloadThemes={() => {}}
    onClose={() => {}} onChanged={onChanged} onError={() => {}} at="ext:installed"
    account={null} accountUnread="" reloadAccount={() => {}}
  />;
}

function pane() {
  return within(document.querySelector<HTMLElement>(".addsrv")!);
}

async function inspect(source: string) {
  await userEvent.type(pane().getByRole("textbox"), source);
  await userEvent.click(screen.getByRole("button", { name: "查看内容" }));
}

it.each(["success", "failure"])("isolates an old parse response after replacing the Settings port (%s)", async (outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  const next = new MockPort() as unknown as AgentPort;
  const old = deferred<McpDraft>();
  const current = deferred<McpDraft>();
  vi.spyOn(port, "parseMcp").mockImplementationOnce(() => old.promise);
  const parse = vi.spyOn(next, "parseMcp").mockImplementationOnce(() => current.promise);
  const install = vi.spyOn(next, "installMcp").mockResolvedValue(result("new-docs"));
  const onChanged = vi.fn();
  const view = render(settings(port, onChanged));
  await userEvent.click(await screen.findByRole("button", { name: "接入服务" }));
  await inspect("https://example.test/old");
  view.rerender(settings(next, onChanged));
  const input = pane().getByRole<HTMLTextAreaElement>("textbox");
  expect(input.value).toBe("");
  expect(document.activeElement).toBe(input);
  await inspect("https://example.test/new");
  await act(async () => outcome === "success" ? old.finish(oldDraft) : old.fail());
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "读取中…" }).disabled).toBe(true);
  expect(screen.queryByText("old-docs")).toBeNull();
  expect(screen.queryByText(failure)).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: "读取中…" }));
  expect(parse).toHaveBeenCalledTimes(1);
  expect(onChanged).not.toHaveBeenCalled();
  await act(async () => current.finish(newDraft));
  await userEvent.click(screen.getByRole("button", { name: "接入" }));
  expect(install).toHaveBeenCalledExactlyOnceWith(newDraft.servers[0], "user");
  expect(onChanged).toHaveBeenCalledTimes(1);
});

it.each(["confirm", "error", "done"])("clears previous connection %s state before another MCP installation", async (stage) => {
  const port = new MockPort() as unknown as AgentPort;
  const next = new MockPort() as unknown as AgentPort;
  const parse = vi.spyOn(port, "parseMcp").mockResolvedValue(oldDraft);
  if (stage === "error") parse.mockRejectedValueOnce(new Error(failure));
  vi.spyOn(port, "installMcp").mockResolvedValue(result("old-docs"));
  const nextParse = vi.spyOn(next, "parseMcp").mockResolvedValue(newDraft);
  const nextInstall = vi.spyOn(next, "installMcp").mockResolvedValue(result("new-docs"));
  const draw = (active: AgentPort) => <AddServer port={active} canProject onClose={() => {}} onInstalled={() => {}} />;
  const view = render(draw(port));
  await inspect("https://example.test/old");
  if (stage !== "error") {
    await userEvent.click(screen.getByRole("radio", { name: /写进仓库/ }));
    if (stage === "done") await userEvent.click(screen.getByRole("button", { name: "接入" }));
  }
  view.rerender(draw(next));
  expect(pane().getByRole<HTMLTextAreaElement>("textbox").value).toBe("");
  expect(screen.queryByText("old-docs")).toBeNull();
  expect(screen.queryByText(failure)).toBeNull();
  await inspect("https://example.test/new");
  expect(screen.getByRole("radio", { name: /^我的/ }).getAttribute("aria-checked")).toBe("true");
  await userEvent.click(screen.getByRole("button", { name: "接入" }));
  expect(nextParse).toHaveBeenCalledExactlyOnceWith("https://example.test/new");
  expect(nextInstall).toHaveBeenCalledExactlyOnceWith(newDraft.servers[0], "user");
});

it.each(["success", "failure"])("keeps the replacement Settings installation busy after old apply settles (%s)", async (outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  const next = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "parseMcp").mockResolvedValue(oldDraft);
  vi.spyOn(next, "parseMcp").mockResolvedValue(newDraft);
  const old = deferred<McpInstallResult>();
  const current = deferred<McpInstallResult>();
  const original = vi.spyOn(port, "installMcp").mockImplementationOnce(() => old.promise);
  const replacement = vi.spyOn(next, "installMcp").mockImplementationOnce(() => current.promise);
  const reads = vi.spyOn(next, "skills");
  const onChanged = vi.fn();
  const view = render(settings(port, onChanged));
  await userEvent.click(await screen.findByRole("button", { name: "接入服务" }));
  await inspect("https://example.test/old");
  await userEvent.click(screen.getByRole("button", { name: "接入" }));
  view.rerender(settings(next, onChanged));
  await inspect("https://example.test/new");
  await userEvent.click(screen.getByRole("button", { name: "接入" }));
  const count = reads.mock.calls.length;
  await act(async () => outcome === "success" ? old.finish(result("old-docs")) : old.fail());
  const applying = screen.getByRole<HTMLButtonElement>("button", { name: "连接中…" });
  expect(applying.disabled).toBe(true);
  expect(screen.queryByText("old-docs")).toBeNull();
  expect(screen.queryByText(failure)).toBeNull();
  expect(onChanged).not.toHaveBeenCalled();
  expect(reads).toHaveBeenCalledTimes(count);
  await userEvent.click(applying);
  expect(original).toHaveBeenCalledTimes(1);
  expect(replacement).toHaveBeenCalledTimes(1);
  await act(async () => current.finish(result("new-docs")));
  expect(screen.getByRole("button", { name: "完成" })).toBeTruthy();
  expect(onChanged).toHaveBeenCalledTimes(1);
  expect(reads).toHaveBeenCalledTimes(count + 1);
});

it("retains completion notification after returning to the original Settings port", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const next = new MockPort() as unknown as AgentPort;
  const old = deferred<McpInstallResult>();
  vi.spyOn(port, "parseMcp").mockResolvedValue(oldDraft);
  vi.spyOn(port, "installMcp").mockImplementationOnce(() => old.promise);
  const reads = vi.spyOn(port, "skills");
  const onChanged = vi.fn();
  const view = render(settings(port, onChanged));
  await userEvent.click(await screen.findByRole("button", { name: "接入服务" }));
  await inspect("https://example.test/old");
  await userEvent.click(screen.getByRole("button", { name: "接入" }));
  view.rerender(settings(next, onChanged));
  view.rerender(settings(port, onChanged));
  expect(pane().getByRole<HTMLTextAreaElement>("textbox").value).toBe("");
  const count = reads.mock.calls.length;
  await act(async () => old.finish(result("old-docs")));
  expect(pane().getByRole<HTMLTextAreaElement>("textbox").value).toBe("");
  expect(screen.queryByRole("button", { name: "完成" })).toBeNull();
  expect(onChanged).toHaveBeenCalledTimes(1);
  expect(reads).toHaveBeenCalledTimes(count + 1);
});

it("finishes an already confirmed batch on its original port after switching Settings", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const next = new MockPort() as unknown as AgentPort;
  const first = deferred<McpInstallResult>();
  const draft: McpDraft = { servers: [oldDraft.servers[0]!, { ...oldDraft.servers[0]!, name: "old-search" }], risks: [] };
  vi.spyOn(port, "parseMcp").mockResolvedValue(draft);
  const install = vi.spyOn(port, "installMcp").mockImplementationOnce(() => first.promise).mockResolvedValueOnce(result("old-search"));
  const replacement = vi.spyOn(next, "installMcp");
  const onChanged = vi.fn();
  const view = render(settings(port, onChanged));
  await userEvent.click(await screen.findByRole("button", { name: "接入服务" }));
  await inspect("https://example.test/old");
  await userEvent.click(screen.getByRole("button", { name: "接入" }));
  view.rerender(settings(next, onChanged));
  await act(async () => first.finish(result("old-docs")));
  expect(install).toHaveBeenNthCalledWith(1, draft.servers[0], "user");
  expect(install).toHaveBeenNthCalledWith(2, draft.servers[1], "user");
  expect(replacement).not.toHaveBeenCalled();
  expect(onChanged).not.toHaveBeenCalled();
  expect(pane().getByRole<HTMLTextAreaElement>("textbox").value).toBe("");
  expect(screen.queryByText("old-search")).toBeNull();
});

it("keeps the same port's pending confirmation and project scope across rerenders", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const pending = deferred<McpInstallResult>();
  vi.spyOn(port, "parseMcp").mockResolvedValue(oldDraft);
  const install = vi.spyOn(port, "installMcp").mockImplementationOnce(() => pending.promise);
  const onInstalled = vi.fn();
  const draw = () => <AddServer port={port} canProject onClose={() => {}} onInstalled={onInstalled} />;
  const view = render(draw());
  await inspect("https://example.test/old");
  await userEvent.click(screen.getByRole("radio", { name: /写进仓库/ }));
  await userEvent.click(screen.getByRole("button", { name: "接入" }));
  view.rerender(draw());
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "连接中…" }).disabled).toBe(true);
  expect(install).toHaveBeenCalledExactlyOnceWith(oldDraft.servers[0], "project");
  await act(async () => pending.finish(result("old-docs")));
  expect(screen.getByRole("button", { name: "完成" })).toBeTruthy();
  expect(onInstalled).toHaveBeenCalledTimes(1);
});
