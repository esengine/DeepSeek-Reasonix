// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../testkit";
import { boot, STORAGE } from "../../i18n";
import { MockHub } from "../../port/mock_hub";
import type { RuntimeView } from "../../port/hub";
import { closeSeatDrawer, toggleSeatDrawer } from "../../state/teamDrawer";
import { TeamRail } from "./TeamRail";
import { TeamSection } from "./TeamSection";
import { TeamLayer } from "./TeamLayer";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
  HTMLDialogElement.prototype.showModal = function () { this.open = true; };
  HTMLDialogElement.prototype.close = function () { this.open = false; };
});
afterEach(() => {
  cleanup();
  closeSeatDrawer();
  vi.restoreAllMocks();
});

async function seeded() {
  const hub = new MockHub();
  const team = hub.team();
  await team.bootstrap("Chief");
  await team.createAgent({ name: "Ada" });
  await team.createGroup({ name: "Launch", members: ["a-chief", "a-1"] });
  team.addShared("team", { name: "house-style", description: "write in plain English" });
  team.addShared("group", { name: "launch-date", description: "the launch is on Friday" }, "g-2");
  return { hub, team };
}

it("the team's shared memory is listed in settings, and forgetting one takes two presses", async () => {
  const { hub, team } = await seeded();
  render(<TeamSection hub={hub} />);
  const list = await screen.findByRole("list", { name: "团队共享记忆" });
  expect(within(list).getByText("write in plain English")).toBeTruthy();
  await userEvent.click(within(list).getByRole("button", { name: "忘掉 house-style" }));
  expect(team.calls.some((c) => c.method === "forgetShared")).toBe(false);
  await userEvent.click(within(list).getByRole("button", { name: "确认忘掉 house-style" }));
  expect(team.calls.at(-1)).toEqual({ method: "forgetShared", args: ["team", "house-style", undefined] });
  await waitFor(() => expect(screen.getByText("团队还没有共享记忆")).toBeTruthy());
});

it("a group's settings list what its members remember", async () => {
  const { hub } = await seeded();
  render(<TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />);
  await userEvent.click(await screen.findByRole("button", { name: "Launch 的设置" }));
  const dialog = await screen.findByRole("dialog", { name: "群聊设置" });
  const list = await within(dialog).findByRole("list", { name: "群记忆" });
  expect(within(list).getByText("the launch is on Friday")).toBeTruthy();
});

it("the details say how much the agent remembers and lead to its memory", async () => {
  const { hub } = await seeded();
  const rt: RuntimeView = { id: "r2", base: "/rt/r2", root: "/team/a-1", name: "a-1", team: { agent: "a-1" } };
  const port = hub.portFor(rt);
  const count = (await port.memories()).memories.length;
  const onMemory = vi.fn();
  render(<TeamLayer hub={hub} active="r2" seat={rt.team} root={rt.root} port={port} onMemory={onMemory} onAdoptPane={vi.fn()} />);
  toggleSeatDrawer();
  const drawer = await screen.findByRole("complementary", { name: "Ada 的详情" });
  expect(await within(drawer).findByText(`${count} 条`)).toBeTruthy();
  await userEvent.click(within(drawer).getByRole("button", { name: "管理记忆" }));
  expect(onMemory).toHaveBeenCalled();
});
