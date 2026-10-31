// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import "../testkit";
import { boot, STORAGE } from "../../i18n";
import { MockHub } from "../../port/mock_hub";
import { MockPort } from "../../port/mock";
import type { RuntimeView } from "../../port/hub";
import { Chrome } from "../Chrome";
import { closeGroupStage, openGroupStage } from "../../state/teamStage";

// An agent's pane runs in a folder the host gave it. The chrome names the
// agent instead, and offers no door into that folder as a project: entering it
// would make the agent's folder one of the user's projects.

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(() => {
  cleanup();
  closeGroupStage();
});

function draw(hub: MockHub, seat?: RuntimeView["team"], port: MockPort | null = null) {
  render(
    <Chrome
      port={port}
      workspace={{ current: "/home/u/Reasonix Team/a-chief", isolated: true, canSwitch: true, canIsolate: true, recents: [] }}
      status={{ preset: "balanced", toolApprovalMode: "ask", workspaceRoot: "/home/u/Reasonix Team/a-chief", sessionPath: "/s.jsonl" } as never}
      title="会话" steer={0} account={null} theme="dark" onTheme={vi.fn()} onFind={vi.fn()}
      view="flow" rows={0} surfaces={0} onView={vi.fn()} hub={hub} seat={seat} onError={vi.fn()}
    />,
  );
}

it("names the agent whose seat the pane holds, not its folder", async () => {
  const hub = new MockHub();
  await hub.team().bootstrap("Chief");
  const port = new MockPort();
  vi.spyOn(port, "workspaces").mockResolvedValue({ current: "/home/u/Reasonix Team/a-chief", isolated: true, canSwitch: true, canIsolate: true, recents: [] });
  draw(hub, { agent: "a-chief" }, port);
  expect(await screen.findByText("Chief")).toBeTruthy();
  expect(screen.queryByText("a-chief")).toBeNull();
  expect(screen.queryByRole("button", { name: "打开项目文件夹" })).toBeNull();
  expect(screen.queryByText("独立工作树")).toBeNull();
});

it("names the group as well for a member's group conversation", async () => {
  const hub = new MockHub();
  const team = hub.team();
  await team.bootstrap("Chief");
  await team.createAgent({ name: "Ada" });
  const g = (await team.createGroup({ name: "Launch", members: ["a-chief", "a-1"] })).groups[0].id;
  draw(hub, { agent: "a-1", group: g });
  expect(await screen.findByText("Ada")).toBeTruthy();
  expect(await screen.findByText("Launch")).toBeTruthy();
});

it("keeps the folder for a pane outside the team", async () => {
  draw(new MockHub());
  expect(await screen.findByText("a-chief")).toBeTruthy();
  expect(screen.getByRole("button", { name: "打开项目文件夹" })).toBeTruthy();
});

it("names the group while its chat covers the panes, not the pane underneath", async () => {
  const hub = new MockHub();
  const team = hub.team();
  await team.bootstrap("Chief");
  await team.createAgent({ name: "Ada" });
  const g = (await team.createGroup({ name: "Launch", members: ["a-chief", "a-1"] })).groups[0].id;
  act(() => openGroupStage(g));
  draw(hub, { agent: "a-chief" });
  expect(await screen.findByText("Launch")).toBeTruthy();
  expect(screen.queryByText("Chief")).toBeNull();
  expect(screen.queryByText("会话")).toBeNull();
  act(() => closeGroupStage());
  expect(await screen.findByText("Chief")).toBeTruthy();
  expect(screen.getByText("会话")).toBeTruthy();
});
