// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { AddServer } from "./AddServer";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { AgentPort, McpDraft, McpInstallResult, McpInstallScope, SessionStatus } from "../port/port";

afterEach(cleanup);

const draft: McpDraft = { servers: [{ name: "docs", transport: "http", url: "https://example.test/mcp" }], risks: [] };
const failure = "service could not be connected";
const result: McpInstallResult = { name: "docs", state: "ready", toolCount: 1, action: "installed", message: "" };
const names: Record<McpInstallScope, RegExp> = { user: /^我的/, local: /^仅当前项目/, project: /^写进仓库/ };

function deferred() {
  let finish!: (value: McpInstallResult) => void;
  let fail!: (error: Error) => void;
  const promise = new Promise<McpInstallResult>((resolve, reject) => { finish = resolve; fail = reject; });
  return { promise, finish: () => finish(result), fail: () => fail(new Error(failure)) };
}

async function confirm() {
  const pane = within(document.querySelector<HTMLElement>(".addsrv")!);
  await userEvent.type(pane.getByRole("textbox"), "https://example.test/mcp");
  await userEvent.click(pane.getByRole("button", { name: "查看内容" }));
}

it.each((Object.keys(names) as McpInstallScope[]).flatMap((scope) => ["success", "failure"].map((outcome) => [scope, outcome] as const)))(
  "keeps the confirmed %s installation location while pending (%s)", async (scope, outcome) => {
    const port = new MockPort() as unknown as AgentPort;
    vi.spyOn(port, "parseMcp").mockResolvedValue(draft);
    const pending = deferred();
    const install = vi.spyOn(port, "installMcp").mockImplementationOnce(() => pending.promise);
    const onInstalled = vi.fn();
    const onClose = vi.fn();
    render(<AddServer port={port} canProject onClose={onClose} onInstalled={onInstalled} />);
    await confirm();
    const selected = screen.getByRole<HTMLButtonElement>("radio", { name: names[scope] });
    await userEvent.click(selected);
    await userEvent.click(screen.getByRole("button", { name: "接入" }));
    for (const other of (Object.keys(names) as McpInstallScope[]).filter((candidate) => candidate !== scope)) {
      await userEvent.click(screen.getByRole("radio", { name: names[other] }));
      expect(selected.getAttribute("aria-checked")).toBe("true");
    }
    expect(screen.getAllByRole<HTMLButtonElement>("radio").every((radio) => radio.disabled)).toBe(true);
    await userEvent.click(screen.getByRole("button", { name: "返回" }));
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "连接中…" }).disabled).toBe(true);
    expect(document.querySelector(".addsrv")!.getAttribute("aria-busy")).toBe("true");
    expect(install).toHaveBeenCalledExactlyOnceWith(draft.servers[0], scope);
    expect(onClose).not.toHaveBeenCalled();
    await act(async () => outcome === "success" ? pending.finish() : pending.fail());
    if (outcome === "success") {
      expect(screen.getByRole<HTMLButtonElement>("button", { name: "完成" }).disabled).toBe(false);
      expect(onInstalled).toHaveBeenCalledTimes(1);
    } else {
      expect(screen.getByText(failure)).toBeTruthy();
      expect(screen.getAllByRole<HTMLButtonElement>("radio").every((radio) => !radio.disabled)).toBe(true);
      expect(selected.getAttribute("aria-checked")).toBe("true");
      expect(document.querySelector(".addsrv")!.getAttribute("aria-busy")).toBe("false");
      expect(onInstalled).not.toHaveBeenCalled();
      await userEvent.click(screen.getByRole("button", { name: "返回" }));
      expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe("https://example.test/mcp");
      expect(screen.getByRole<HTMLButtonElement>("button", { name: "查看内容" }).disabled).toBe(false);
    }
  },
);

it("restores only the available locations after failure without a project", async () => {
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "parseMcp").mockResolvedValue(draft);
  const pending = deferred();
  vi.spyOn(port, "installMcp").mockImplementationOnce(() => pending.promise);
  render(<AddServer port={port} canProject={false} onClose={() => {}} onInstalled={() => {}} />);
  await confirm();
  await userEvent.click(screen.getByRole("button", { name: "接入" }));
  await userEvent.click(screen.getByRole("button", { name: "返回" }));
  expect(screen.queryByRole("textbox")).toBeNull();
  expect(screen.getAllByRole<HTMLButtonElement>("radio").every((radio) => radio.disabled)).toBe(true);
  await act(async () => pending.fail());
  expect(screen.getByRole<HTMLButtonElement>("radio", { name: names.user }).disabled).toBe(false);
  expect(screen.getByRole<HTMLButtonElement>("radio", { name: names.local }).disabled).toBe(true);
  expect(screen.getByRole<HTMLButtonElement>("radio", { name: names.project }).disabled).toBe(true);
});

it("lets Escape dismiss the Settings form while its confirmed installation still completes", async () => {
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "parseMcp").mockResolvedValue(draft);
  const pending = deferred();
  const install = vi.spyOn(port, "installMcp").mockImplementationOnce(() => pending.promise);
  const onChanged = vi.fn();
  render(<Settings
    hub={new MockHub() as never} port={port}
    status={{ preset: "balanced", toolApprovalMode: "ask", workspaceRoot: "/workspace/main" } as SessionStatus}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{} as never} onLook={() => {}} reloadThemes={() => {}}
    onClose={() => {}} onChanged={onChanged} onError={() => {}} at="ext:installed"
    account={null} accountUnread="" reloadAccount={() => {}}
  />);
  await userEvent.click(await screen.findByRole("button", { name: "接入服务" }));
  await confirm();
  await userEvent.click(screen.getByRole("button", { name: "接入" }));
  await userEvent.keyboard("{Escape}");
  expect(document.querySelector(".addsrv")).toBeNull();
  expect(install).toHaveBeenCalledExactlyOnceWith(draft.servers[0], "user");
  await act(async () => pending.finish());
  expect(onChanged).toHaveBeenCalledTimes(1);
});
