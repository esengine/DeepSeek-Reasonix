// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { AddServer } from "./AddServer";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { AgentPort, CapabilityScope, McpDraft, McpEntry, McpInstallResult, SessionStatus } from "../port/port";

afterEach(cleanup);

const failure = "MCP configuration could not be saved";
const draft: McpDraft = {
  servers: ["saved-docs", "failed-search", "remaining-notes"].map((name) => ({ name, transport: "http", url: `https://example.test/${name}` })),
  risks: [],
};
const result = (name: string, state: string): McpInstallResult => ({ name, state, toolCount: state === "ready" ? 1 : 0, action: "installed", message: "" });
const scopes: CapabilityScope[] = [
  { root: "/workspace/main", name: "main", key: "main", repo: false, current: true, overrides: 0 },
  { root: "/workspace/other", name: "other", key: "other", repo: false, current: false, overrides: 0 },
];

function settings(port: AgentPort, onChanged: () => void) {
  return <Settings
    hub={new MockHub() as never} port={port}
    status={{ preset: "balanced", toolApprovalMode: "ask", workspaceRoot: "/workspace/main" } as SessionStatus}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{} as never} onLook={() => {}} reloadThemes={() => {}}
    onClose={() => {}} onChanged={onChanged} onError={() => {}} at="ext:installed"
    account={null} accountUnread="" reloadAccount={() => {}}
  />;
}

async function preview() {
  const pane = within(document.querySelector<HTMLElement>(".addsrv")!);
  await userEvent.type(pane.getByRole("textbox"), "https://example.test/batch");
  await userEvent.click(pane.getByRole("button", { name: "查看内容" }));
  await userEvent.click(pane.getByRole("button", { name: "接入" }));
}

it.each(["ready", "action_required"])("refreshes real Settings inventory after a %s result and a later exception", async (state) => {
  const port = new MockPort() as unknown as AgentPort;
  const inventory: McpEntry[] = [];
  vi.spyOn(port, "parseMcp").mockResolvedValue(draft);
  const reads = vi.spyOn(port, "mcp").mockImplementation(async () => ({ servers: [...inventory], scope: scopes[0]!, live: true }));
  const install = vi.spyOn(port, "installMcp")
    .mockImplementationOnce(async () => {
      inventory.push({ name: "saved-docs", enabled: true, state: state === "ready" ? "ready" : "failed", tools: 0, transport: "http" });
      return result("saved-docs", state);
    })
    .mockRejectedValueOnce(new Error(failure));
  const onChanged = vi.fn();
  render(settings(port, onChanged));
  await userEvent.click(await screen.findByRole("button", { name: "接入服务" }));
  const count = reads.mock.calls.length;
  await preview();
  expect(screen.getByText(failure)).toBeTruthy();
  expect(install).toHaveBeenCalledTimes(2);
  expect(install).toHaveBeenNthCalledWith(1, draft.servers[0], "user");
  expect(install).toHaveBeenNthCalledWith(2, draft.servers[1], "user");
  expect(reads).toHaveBeenCalledTimes(count + 1);
  expect(onChanged).toHaveBeenCalledTimes(1);
  const control = await screen.findByRole("switch", { name: "关闭 saved-docs" });
  const row = within(control.closest<HTMLElement>(".srv")!);
  expect(row.getByText("saved-docs")).toBeTruthy();
  expect(row.queryByText("failed-search")).toBeNull();
  expect(row.queryByText("remaining-notes")).toBeNull();
  expect(screen.queryByRole("button", { name: "完成" })).toBeNull();
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "接入" }).disabled).toBe(false);
});

it.each(["first-exception", "issue-exception", "all-issues", "all-ready"])("notifies only saved results in a %s batch", async (outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "parseMcp").mockResolvedValue(draft);
  const install = vi.spyOn(port, "installMcp");
  if (outcome === "first-exception") install.mockRejectedValueOnce(new Error(failure));
  else if (outcome === "issue-exception") install.mockResolvedValueOnce(result("saved-docs", "issue")).mockRejectedValueOnce(new Error(failure));
  else install.mockImplementation(async (server) => result(server.name, outcome === "all-ready" ? "ready" : "issue"));
  const onInstalled = vi.fn();
  render(<AddServer port={port} canProject onClose={() => {}} onInstalled={onInstalled} />);
  await preview();
  const completed = outcome === "all-ready" || outcome === "all-issues";
  expect(install).toHaveBeenCalledTimes(completed ? 3 : outcome === "first-exception" ? 1 : 2);
  expect(onInstalled).toHaveBeenCalledTimes(outcome === "all-ready" ? 1 : 0);
  if (outcome === "all-ready") expect(screen.getByRole("button", { name: "完成" })).toBeTruthy();
  else if (outcome === "all-issues") {
    expect(screen.queryByRole("button", { name: "完成" })).toBeNull();
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "接入" }).disabled).toBe(false);
  } else expect(screen.getByText(failure)).toBeTruthy();
});

it("refreshes the current managed project after an unmounted batch partially finishes", async () => {
  const port = new MockPort() as unknown as AgentPort;
  let fail!: (error: Error) => void;
  const pending = new Promise<McpInstallResult>((_, reject) => { fail = reject; });
  vi.spyOn(port, "parseMcp").mockResolvedValue(draft);
  vi.spyOn(port, "capabilityScopes").mockResolvedValue(scopes);
  vi.spyOn(port, "mcp").mockImplementation(async (root) => ({ servers: [], scope: scopes[root ? 1 : 0]!, live: !root }));
  vi.spyOn(port, "installMcp").mockResolvedValueOnce(result("saved-docs", "ready")).mockImplementationOnce(() => pending);
  const skills = vi.spyOn(port, "skills");
  const onChanged = vi.fn();
  render(settings(port, onChanged));
  await userEvent.click(await screen.findByRole("button", { name: "接入服务" }));
  await preview();
  await userEvent.click(within(document.querySelector<HTMLElement>(".scopebar")!).getByRole("button", { name: "main" }));
  await userEvent.click(screen.getByRole("option", { name: /other/ }));
  await screen.findByText("/workspace/other");
  expect(document.querySelector(".addsrv")).toBeNull();
  const reads = skills.mock.calls.length;
  await act(async () => fail(new Error(failure)));
  expect(onChanged).toHaveBeenCalledTimes(1);
  expect(skills).toHaveBeenCalledTimes(reads + 1);
  expect(skills).toHaveBeenLastCalledWith("/workspace/other");
  expect(screen.queryByText(failure)).toBeNull();
});
