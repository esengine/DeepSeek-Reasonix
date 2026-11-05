// @vitest-environment jsdom
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import "./testkit";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { AgentPort, SessionStatus } from "../port/port";
import { Settings } from "./Settings";

afterEach(cleanup);

function draw(at: string) {
  const port = new MockPort() as unknown as AgentPort;
  render(
    <Settings hub={new MockHub() as never} port={port} status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
      theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
      look={{} as never} onLook={() => {}} reloadThemes={() => {}} onClose={() => {}} onChanged={() => {}} onError={() => {}}
      at={at} account={null} accountUnread="" reloadAccount={() => {}} />,
  );
  return port;
}

const pane = () => screen.getByRole("complementary", { name: "详情" });

it("opens as a page of its own: no settings navigation, the list beside the first item's detail", async () => {
  draw("extensions");
  expect(screen.getByRole("heading", { level: 1, name: "扩展" })).toBeTruthy();
  expect(screen.queryByRole("tablist", { name: /设置/ })).toBeNull();
  await waitFor(() => expect(within(pane()).getByText("review-kit")).toBeTruthy());
  expect(within(pane()).getByText("已启用")).toBeTruthy();
  expect(within(pane()).getByText("启用、停用或更新后，从下一轮开始生效；正在跑的这一轮不受影响。")).toBeTruthy();
});

it("shows what is chosen in the list, and acts on it from the detail", async () => {
  const port = draw("extensions");
  const exported = vi.spyOn(port, "exportPlugin");
  await waitFor(() => expect(document.querySelector('[data-action-click="extensions.pick"][data-target="notion-bridge"]')).toBeTruthy());
  await userEvent.click(within(document.querySelector<HTMLElement>('[data-action-click="extensions.pick"][data-target="notion-bridge"]')!).getByText("notion-bridge"));
  await waitFor(() => expect(within(pane()).getByText("notion-bridge")).toBeTruthy());
  expect(document.querySelector('[data-extension-name="notion-bridge"][data-picked]')).toBeTruthy();
  await userEvent.click(within(pane()).getByRole("button", { name: "导出" }));
  await waitFor(() => expect(exported).toHaveBeenCalledWith("notion-bridge"));
});

it("keeps the extensions section of Settings as it was, unfolding rows in place", async () => {
  draw("ext");
  await screen.findAllByText("review-kit");
  expect(screen.queryByRole("complementary", { name: "详情" })).toBeNull();
});

it("opens usage as a page of its own, with what it spent and the export", async () => {
  draw("spend");
  expect(screen.getByRole("heading", { level: 1, name: "用量" })).toBeTruthy();
  expect(await screen.findByRole("button", { name: "导出 CSV" })).toBeTruthy();
  expect(screen.queryByRole("button", { name: /用量与成本/ })).toBeNull();
});
