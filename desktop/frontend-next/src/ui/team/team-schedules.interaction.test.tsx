// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../testkit";
import { boot, STORAGE } from "../../i18n";
import { MockHub } from "../../port/mock_hub";
import { OriginLine } from "./OriginLine";
import { TeamSection } from "./TeamSection";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
  HTMLDialogElement.prototype.showModal = function () { this.open = true; };
  HTMLDialogElement.prototype.close = function () { this.open = false; };
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

async function seeded() {
  const hub = new MockHub();
  const team = hub.team();
  await team.bootstrap("Chief");
  await team.createAgent({ name: "Ada" });
  await team.createGroup({ name: "Launch", members: ["a-chief", "a-1"] });
  render(<TeamSection hub={hub} />);
  return { hub, team };
}

it("a daily task is set for an agent and listed with when it runs", async () => {
  const { team } = await seeded();
  await userEvent.click(await screen.findByRole("button", { name: "新建定时任务" }));
  const dialog = await screen.findByRole("dialog", { name: "新建定时任务" });
  await userEvent.selectOptions(within(dialog).getByLabelText("发给"), "agent:a-1");
  await userEvent.type(within(dialog).getByLabelText("名称"), "晨报");
  await userEvent.type(within(dialog).getByLabelText("要做的事"), "总结昨天的进展");
  await userEvent.selectOptions(within(dialog).getByLabelText("重复方式"), "daily");
  await userEvent.clear(within(dialog).getByLabelText("时间"));
  await userEvent.type(within(dialog).getByLabelText("时间"), "09:00");
  await userEvent.click(within(dialog).getByRole("button", { name: "保存" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(team.calls.find((c) => c.method === "saveSchedule")?.args[0]).toMatchObject({
    name: "晨报", agent: "a-1", prompt: "总结昨天的进展", repeat: "daily", clock: "09:00", enabled: true,
  });
  const list = await screen.findByRole("list", { name: "定时任务" });
  expect(await within(list).findByText("晨报")).toBeTruthy();
  expect(within(list).getByText(/Ada · 每天 09:00/)).toBeTruthy();
});

it("a task is paused with its switch and removed with two presses", async () => {
  const { team } = await seeded();
  await team.saveSchedule({ name: "周报", group: "g-2", prompt: "写周报", repeat: "weekly", clock: "17:00", days: [5], enabled: true });
  const list = await screen.findByRole("list", { name: "定时任务" });
  expect(await within(list).findByText(/Launch · 每周五 17:00/)).toBeTruthy();
  await userEvent.click(within(list).getByRole("switch", { name: "启用 周报" }));
  expect(team.calls.at(-1)?.args[0]).toMatchObject({ name: "周报", enabled: false });
  await userEvent.click(within(list).getByRole("button", { name: "删除 周报" }));
  await userEvent.click(within(list).getByRole("button", { name: "确认删除 周报" }));
  expect(team.calls.at(-1)?.method).toBe("deleteSchedule");
  await waitFor(() => expect(within(block()).getByText("还没有定时任务")).toBeTruthy());
});

const block = () => document.getElementById("set-team-schedules")!;

it("a turn a scheduled task started says so", () => {
  render(<OriginLine origin={{ kind: "schedule" }} text={'<scheduled-task name="晨报" id="s-1">\n总结昨天的进展\n</scheduled-task>'} />);
  expect(screen.getByRole("button", { name: "定时任务" })).toBeTruthy();
});
