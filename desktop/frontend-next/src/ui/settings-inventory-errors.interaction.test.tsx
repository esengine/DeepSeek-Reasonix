// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { AgentPort, SessionStatus } from "../port/port";

afterEach(cleanup);

function draw(port: AgentPort) {
  return render(<Settings
    hub={new MockHub() as never} port={port}
    status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{} as never} onLook={() => {}} reloadThemes={() => {}}
    onClose={() => {}} onChanged={() => {}} onError={() => {}} at="ext:installed"
    account={null} accountUnread="" reloadAccount={() => {}}
  />);
}

const inventories = [
  { method: "plugins", group: "plugins", empty: "尚未安装插件包。", entry: "review-kit" },
  { method: "mcp", group: "mcp", empty: "尚未接入外部服务。", entry: "context7" },
  { method: "skills", group: "skills", empty: "当前工作目录下没有技能。", entry: "/review" },
] as const;

it.each(inventories)("shows a $method read failure and retries the current inventory", async ({ method, group, empty, entry }) => {
  const port = new MockPort() as unknown as AgentPort;
  const result = await port[method]();
  let finish!: (value: typeof result) => void;
  const pending = new Promise<typeof result>((resolve) => { finish = resolve; });
  const read = vi.spyOn(port, method).mockRejectedValueOnce(new Error("inventory read unavailable")).mockImplementationOnce(() => pending as never);
  draw(port);
  const pane = within(document.querySelector<HTMLElement>(`#set-${group}`)!);
  expect(await pane.findByRole("alert")).toHaveProperty("textContent", expect.stringContaining("inventory read unavailable"));
  expect(pane.queryByText(empty)).toBeNull();
  const retry = pane.getByRole<HTMLButtonElement>("button", { name: "重试" });
  await waitFor(() => expect(retry.disabled).toBe(false));
  retry.focus();
  await userEvent.keyboard("{Enter}");
  await waitFor(() => expect(read).toHaveBeenCalledTimes(2));
  expect(pane.queryByText(empty)).toBeNull();
  await act(async () => finish(result));
  expect(await pane.findByText(entry)).toBeTruthy();
  expect(pane.queryByRole("alert")).toBeNull();
  expect(pane.queryByRole("button", { name: "重试" })).toBeNull();
});

it.each(inventories)("reports an empty $method inventory only after its read succeeds", async ({ method, group, empty }) => {
  const port = new MockPort() as unknown as AgentPort;
  const result = await port[method]();
  const emptyResult = method === "plugins" ? [] : method === "mcp" ? { ...result, servers: [] } : { ...result, skills: [] };
  let finish!: () => void;
  vi.spyOn(port, method).mockImplementationOnce(() => new Promise<never>((resolve) => { finish = () => resolve(emptyResult as never); }));
  draw(port);
  const pane = within(document.querySelector<HTMLElement>(`#set-${group}`)!);
  await waitFor(() => expect(finish).toBeTypeOf("function"));
  expect(pane.queryByText(empty)).toBeNull();
  await act(async () => finish());
  expect(await pane.findByText(empty)).toBeTruthy();
  expect(pane.queryByRole("alert")).toBeNull();
});
