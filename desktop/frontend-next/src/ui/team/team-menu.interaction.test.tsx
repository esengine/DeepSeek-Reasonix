// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../testkit";
import { boot, STORAGE } from "../../i18n";
import { MockHub } from "../../port/mock_hub";
import { closeGroupStage } from "../../state/teamStage";
import { TeamRail } from "./TeamRail";

beforeEach(() => {
  localStorage.clear();
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

async function seeded() {
  const hub = new MockHub();
  const team = hub.team();
  await team.bootstrap("Chief", "首席助理");
  await team.createAgent({ name: "Ada", role: "写手", instructions: "write plainly", avatar: { glyph: "kitty", color: "cat-3" }, model: "m-1" });
  await team.createAgent({ name: "Ben", role: "程序员" });
  await team.createGroup({ name: "Launch", members: ["a-chief", "a-1"], admin: "a-chief" });
  return { hub, team };
}

// The row's own open button, not the settings button beside it.
async function opener(name: RegExp): Promise<HTMLElement> {
  const all = await screen.findAllByRole("button", { name });
  const hit = all.find((b) => b.dataset.actionClick === "team.agent-open" || b.dataset.actionClick === "team.group-open");
  if (!hit) throw new Error(`no row opens ${name}`);
  return hit;
}

const rowOf = async (name: RegExp) => (await opener(name)).closest("li") as HTMLElement;

async function menuFor(name: RegExp) {
  fireEvent.contextMenu(await opener(name));
  return screen.findByRole("menu");
}

it("right-clicking an agent offers its actions, and pinning moves it up", async () => {
  const { hub, team } = await seeded();
  render(<TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />);
  const menu = await menuFor(/^Ben /);
  await userEvent.click(within(menu).getByRole("menuitem", { name: "置顶" }));
  expect(team.calls.at(-1)).toEqual({ method: "updateAgent", args: ["a-2", { pinned: true }] });
  await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
  const rows = screen.getAllByRole("listitem");
  expect(within(rows[1]).getByText("Ben")).toBeTruthy();
});

it("a hidden agent leaves the rail and comes back from the hidden list", async () => {
  const { hub, team } = await seeded();
  render(<TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />);
  await userEvent.click(within(await menuFor(/^Ben /)).getByRole("menuitem", { name: "从侧栏隐藏" }));
  await waitFor(() => expect(screen.queryAllByRole("button", { name: /^Ben / }).filter((b) => b.dataset.actionClick === "team.agent-open")).toHaveLength(0));
  await userEvent.click(screen.getByRole("button", { name: "已隐藏 1 个" }));
  await userEvent.click(within(await menuFor(/^Ben /)).getByRole("menuitem", { name: "在侧栏显示" }));
  expect(team.calls.at(-1)).toEqual({ method: "updateAgent", args: ["a-2", { hidden: false }] });
});

it("duplicating an agent copies its look and work but not its conversation, then opens the copy", async () => {
  const { hub, team } = await seeded();
  const adopt = vi.fn();
  render(<TeamRail hub={hub} active="" onAdoptPane={async (rt) => adopt(await rt)} />);
  await userEvent.click(within(await menuFor(/^Ada /)).getByRole("menuitem", { name: "复制 Agent" }));
  await waitFor(() => expect(adopt).toHaveBeenCalled());
  const create = team.calls.find((c) => c.method === "createAgent" && (c.args[0] as { name: string }).name === "Ada 副本");
  expect(create?.args[0]).toEqual({ name: "Ada 副本", role: "写手", instructions: "write plainly", avatar: { glyph: "kitty", color: "cat-3" }, model: "m-1", folder: undefined, permission: undefined });
  expect(team.calls.at(-1)?.method).toBe("openAgent");
});

it("the main role can be given, and taken back only while another main agent remains", async () => {
  const { hub, team } = await seeded();
  render(<TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />);
  let menu = await menuFor(/^Chief /);
  expect(within(menu).queryByRole("menuitem", { name: "取消主 Agent" })).toBeNull();
  expect(within(menu).queryByRole("menuitem", { name: /删除/ })).toBeNull();
  fireEvent.keyDown(document, { key: "Escape" });
  await userEvent.click(within(await menuFor(/^Ada /)).getByRole("menuitem", { name: "设为主 Agent" }));
  expect(team.calls.at(-1)).toEqual({ method: "updateAgent", args: ["a-1", { main: true }] });
  menu = await menuFor(/^Chief /);
  expect(within(menu).getByRole("menuitem", { name: "取消主 Agent" })).toBeTruthy();
});

it("deleting asks twice", async () => {
  const { hub, team } = await seeded();
  render(<TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />);
  const menu = await menuFor(/^Ben /);
  await userEvent.click(within(menu).getByRole("menuitem", { name: "删除 Agent" }));
  expect(team.calls.some((c) => c.method === "deleteAgent")).toBe(false);
  await userEvent.click(within(menu).getByRole("menuitem", { name: "确认删除 Ben" }));
  expect(team.calls.at(-1)).toEqual({ method: "deleteAgent", args: ["a-2"] });
});

it("an agent that moved behind the window reads as unread until it is opened", async () => {
  const { hub, team } = await seeded();
  render(<TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />);
  expect((await rowOf(/^Ada /)).dataset.unread).toBeUndefined();
  act(() => team.touch("a-1", 2000));
  await waitFor(async () => expect((await rowOf(/^Ada /)).dataset.unread).toBe(""));
  await userEvent.click(await opener(/^Ada /));
  await waitFor(async () => expect((await rowOf(/^Ada /)).dataset.unread).toBeUndefined());
  await userEvent.click(within(await menuFor(/^Ada /)).getByRole("menuitem", { name: "标为未读" }));
  expect((await rowOf(/^Ada /)).dataset.unread).toBe("");
});

it("the menu opens from the keyboard as well", async () => {
  const { hub } = await seeded();
  render(<TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />);
  fireEvent.keyDown(await opener(/^Ada /), { key: "ContextMenu" });
  expect(await screen.findByRole("menu", { name: "Ada 的操作" })).toBeTruthy();
});

it("a group's menu pins, hides and deletes it", async () => {
  const { hub, team } = await seeded();
  render(<TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />);
  await userEvent.click(within(await menuFor(/^Launch/)).getByRole("menuitem", { name: "置顶" }));
  expect(team.calls.at(-1)).toEqual({ method: "updateGroup", args: ["g-3", { pinned: true }] });
  const menu = await menuFor(/^Launch/);
  await userEvent.click(within(menu).getByRole("menuitem", { name: "删除群聊" }));
  await userEvent.click(within(menu).getByRole("menuitem", { name: "确认删除 Launch" }));
  expect(team.calls.at(-1)).toEqual({ method: "deleteGroup", args: ["g-3"] });
});
