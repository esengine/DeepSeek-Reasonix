// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../testkit";
import { boot, STORAGE } from "../../i18n";
import { MockHub } from "../../port/mock_hub";
import type { RuntimeView } from "../../port/hub";
import { closeGroupStage } from "../../state/teamStage";
import { GroupStage } from "./GroupStage";
import { TeamRail } from "./TeamRail";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
  HTMLDialogElement.prototype.showModal = function () { this.open = true; };
  HTMLDialogElement.prototype.close = function () { this.open = false; };
});
afterEach(() => {
  cleanup();
  closeGroupStage();
  vi.restoreAllMocks();
});

async function launch(active = "r1") {
  const hub = new MockHub();
  const team = hub.team();
  await team.bootstrap("Chief");
  await team.createAgent({ name: "Ada" });
  await team.createAgent({ name: "Ben" });
  await team.createGroup({ name: "Launch", members: ["a-chief", "a-1", "a-2"], admin: "a-chief" });
  const adopt = vi.fn<(rt: RuntimeView) => void>();
  const ui = (on: string) => (
    <>
      <TeamRail hub={hub} active={on} onAdoptPane={async (rt) => adopt(await rt)} />
      <GroupStage hub={hub} active={on} onAdoptPane={async (rt) => adopt(await rt)} />
    </>
  );
  const view = render(ui(active));
  await userEvent.click(await screen.findByRole("button", { name: "Launch" }));
  const stage = await screen.findByRole("region", { name: "Launch" });
  return { hub, team, adopt, stage, rerender: (on: string) => view.rerender(ui(on)) };
}

it("a mention picked from the list reaches the kernel as a member id", async () => {
  const { team, stage } = await launch();
  const box = within(stage).getByRole("textbox", { name: "发到群聊" });
  await userEvent.type(box, "@");
  await userEvent.click(within(stage).getByRole("option", { name: "@Ada" }));
  expect(within(stage).getByRole("button", { name: "@Ada ×" })).toBeTruthy();
  await userEvent.type(box, "plan the launch{Enter}");
  await waitFor(() => expect(team.calls.at(-1)).toEqual({ method: "postGroup", args: ["g-3", "plan the launch", ["a-1"], false] }));
  expect(await within(stage).findByText("plan the launch")).toBeTruthy();
  expect((box as HTMLTextAreaElement).value).toBe("");
  expect(within(stage).queryByRole("button", { name: "@Ada ×" })).toBeNull();
});

it("everyone is a choice of its own, not a name", async () => {
  const { team, stage } = await launch();
  const box = within(stage).getByRole("textbox", { name: "发到群聊" });
  await userEvent.type(box, "@");
  await userEvent.click(within(stage).getByRole("option", { name: "@所有人" }));
  await userEvent.type(box, "status?{Enter}");
  await waitFor(() => expect(team.calls.at(-1)).toEqual({ method: "postGroup", args: ["g-3", "status?", [], true] }));
});

it("the log draws late replies and a pause that Continue lifts", async () => {
  const { team, stage } = await launch();
  act(() => team.addLine("g-3", { from: "a-1", kind: "post", text: "draft is up", late: true }));
  expect(await within(stage).findByText("draft is up")).toBeTruthy();
  expect(within(stage).getByText("迟到的回复")).toBeTruthy();
  act(() => team.addLine("g-3", { from: "user", kind: "event", event: "paused", count: 6 }));
  act(() => team.setRound("g-3", { parked: 1 }));
  expect(await within(stage).findByText("Agent 之间已接力 6 次，已暂停")).toBeTruthy();
  await userEvent.click(within(stage).getByRole("button", { name: "继续" }));
  await waitFor(() => expect(team.calls.at(-1)).toEqual({ method: "continueGroup", args: ["g-3"] }));
});

it("the dock stops the speaker and takes the person to a member waiting on them", async () => {
  const { team, stage, adopt } = await launch();
  act(() => team.setRound("g-3", { speaking: "a-2" }));
  expect(await within(stage).findByText("Ben 正在发言")).toBeTruthy();
  await userEvent.click(within(stage).getByRole("button", { name: "停止 Ben" }));
  await waitFor(() => expect(team.calls.at(-1)).toEqual({ method: "stopMember", args: ["g-3", "a-2"] }));
  act(() => team.setActivity("a-1", { state: "needs_you", runtime: "team-g-3-a-1" }));
  await userEvent.click(await within(stage).findByRole("button", { name: "Ada 等你处理" }));
  await waitFor(() => expect(adopt).toHaveBeenCalledWith(expect.objectContaining({ id: "team-g-3-a-1" })));
});

it("opening a pane puts the group chat away", async () => {
  const { rerender } = await launch("r1");
  rerender("r2");
  await waitFor(() => expect(screen.queryByRole("region", { name: "Launch" })).toBeNull());
});

it("a failed turn says why when the kernel's code has words, and opens the member's conversation", async () => {
  const { team, stage, adopt } = await launch();
  act(() => team.addLine("g-3", { from: "a-1", kind: "event", event: "failed", code: "turn.timeout" }));
  expect(await within(stage).findByText("Ada 这一轮失败了：这一轮超时了")).toBeTruthy();
  act(() => team.addLine("g-3", { from: "a-2", kind: "event", event: "failed", code: "turn.failed" }));
  const line = (await within(stage).findByText("Ben 这一轮失败了")).closest(".team-event") as HTMLElement;
  expect(line.textContent).not.toContain("turn.failed");
  await userEvent.click(within(line).getByRole("button", { name: "查看 Ben 的对话" }));
  await waitFor(() => expect(adopt).toHaveBeenCalledWith(expect.objectContaining({ team: { agent: "a-2", group: "g-3" } })));
});

it("a long log opens on its latest lines and pages back on request", async () => {
  const hub = new MockHub();
  const team = hub.team();
  await team.bootstrap("Chief");
  await team.createAgent({ name: "Ada" });
  await team.createGroup({ name: "Long", members: ["a-chief", "a-1"] });
  for (let i = 1; i <= 300; i++) team.addLine("g-2", { from: "a-1", kind: "post", text: `line ${i}` });
  render(
    <>
      <TeamRail hub={hub} active="r1" onAdoptPane={vi.fn()} />
      <GroupStage hub={hub} active="r1" onAdoptPane={vi.fn()} />
    </>,
  );
  await userEvent.click(await screen.findByRole("button", { name: "Long line 300" }));
  const stage = await screen.findByRole("region", { name: "Long" });
  expect(await within(stage).findByText("line 300")).toBeTruthy();
  expect(within(stage).queryByText("line 100")).toBeNull();
  await userEvent.click(within(stage).getByRole("button", { name: "加载更早的消息" }));
  expect(await within(stage).findByText("line 1")).toBeTruthy();
  expect(within(stage).queryByRole("button", { name: "加载更早的消息" })).toBeNull();
});
