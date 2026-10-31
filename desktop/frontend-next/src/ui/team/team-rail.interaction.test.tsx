// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../testkit";
import { boot, STORAGE } from "../../i18n";
import { MockHub } from "../../port/mock_hub";
import { closeGroupStage } from "../../state/teamStage";
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

async function seeded() {
  const hub = new MockHub();
  const team = hub.team();
  await team.bootstrap("Chief", "首席助理");
  await team.createAgent({ name: "Ada", role: "写手" });
  await team.createAgent({ name: "Ben", role: "程序员" });
  return { hub, team };
}

it("an empty team offers Chief, and creating it opens Chief's conversation", async () => {
  const hub = new MockHub();
  const adopt = vi.fn();
  render(<TeamRail hub={hub} active="" onAdoptPane={async (rt) => adopt(await rt)} />);
  await userEvent.click(await screen.findByRole("button", { name: "创建 Chief" }));
  await waitFor(() => expect(adopt).toHaveBeenCalledWith(expect.objectContaining({ id: "team-a-chief" })));
  expect(hub.team().calls.map((c) => c.method)).toEqual(["bootstrap", "openAgent"]);
  expect(await screen.findByText("Chief")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "创建 Chief" })).toBeNull();
});

it("rows say what each agent is doing, and the main agent leads", async () => {
  const { hub, team } = await seeded();
  render(<TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />);
  const rows = await screen.findAllByRole("listitem");
  expect(within(rows[0]).getByText("Chief")).toBeTruthy();
  act(() => team.setActivity("a-1", { state: "working" }));
  act(() => team.setActivity("a-2", { state: "waiting_on", on: ["a-1"] }));
  expect(await screen.findByText("工作中")).toBeTruthy();
  expect(await screen.findByText("等待 Ada")).toBeTruthy();
  act(() => team.setActivity("a-1", { state: "needs_you", runtime: "r9" }));
  expect(await screen.findByText("等你处理")).toBeTruthy();
});

it("opening an agent opens its pane through the kernel and hands it to the window", async () => {
  const { hub, team } = await seeded();
  const adopt = vi.fn();
  render(<TeamRail hub={hub} active="" onAdoptPane={async (rt) => adopt(await rt)} />);
  await userEvent.click(await screen.findByRole("button", { name: "Ada 写手" }));
  await waitFor(() => expect(adopt).toHaveBeenCalledWith(expect.objectContaining({ id: "team-a-1", name: "Ada" })));
  expect(team.calls.at(-1)).toEqual({ method: "openAgent", args: ["a-1"] });
});

it("a new agent is created with its look, and a refusal speaks the kernel's code", async () => {
  const { hub, team } = await seeded();
  render(<TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />);
  await userEvent.click(await screen.findByRole("button", { name: "新建 Agent" }));
  const dialog = await screen.findByRole("dialog", { name: "新建 Agent" });
  await userEvent.type(within(dialog).getByLabelText("名字"), "ada");
  await userEvent.click(within(dialog).getByRole("button", { name: "创建" }));
  expect(await within(dialog).findByRole("alert")).toHaveProperty("textContent", "这个名字已被使用");
  await userEvent.clear(within(dialog).getByLabelText("名字"));
  await userEvent.type(within(dialog).getByLabelText("名字"), "Cy");
  await userEvent.type(within(dialog).getByLabelText("角色"), "审稿");
  await userEvent.click(within(dialog).getByRole("button", { name: "创建" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  const create = team.calls.filter((c) => c.method === "createAgent").at(-1)!;
  expect(create.args[0]).toMatchObject({ name: "Cy", role: "审稿", avatar: { glyph: expect.any(String), color: expect.any(String) } });
  expect(await screen.findByText("Cy")).toBeTruthy();
});

it("the avatar picker keeps shapes and colours apart, and creates what was picked", async () => {
  const { hub, team } = await seeded();
  render(<TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />);
  await userEvent.click(await screen.findByRole("button", { name: "新建 Agent" }));
  const dialog = await screen.findByRole("dialog", { name: "新建 Agent" });
  const shapes = within(dialog).getByRole("group", { name: "形状" });
  const colours = within(dialog).getByRole("group", { name: "颜色" });
  expect(within(shapes).queryByRole("button", { name: "颜色 3" })).toBeNull();
  expect(within(colours).queryByRole("button", { name: "猫咪" })).toBeNull();
  await userEvent.click(within(shapes).getByRole("button", { name: "猫咪" }));
  await userEvent.click(within(colours).getByRole("button", { name: "颜色 3" }));
  expect(within(shapes).getAllByRole("button", { pressed: true })).toHaveLength(1);
  expect(within(colours).getAllByRole("button", { pressed: true })).toHaveLength(1);
  await userEvent.type(within(dialog).getByLabelText("名字"), "Cy");
  await userEvent.click(within(dialog).getByRole("button", { name: "创建" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(team.calls.filter((c) => c.method === "createAgent").at(-1)!.args[0]).toMatchObject({ avatar: { glyph: "kitty", color: "cat-3" } });
});

it("a group is made from ticked members, and an admin answers by default", async () => {
  const { hub, team } = await seeded();
  render(<TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />);
  await userEvent.click(await screen.findByRole("button", { name: "新建群聊" }));
  const dialog = await screen.findByRole("dialog", { name: "新建群聊" });
  const members = within(dialog).getByRole("group", { name: "成员" });
  const create = within(dialog).getByRole("button", { name: "创建" });
  await userEvent.click(within(members).getByRole("checkbox", { name: "Chief" }));
  expect((create as HTMLButtonElement).disabled).toBe(true);
  await userEvent.click(within(members).getByRole("checkbox", { name: "Ada" }));
  expect((create as HTMLButtonElement).disabled).toBe(false);
  await userEvent.selectOptions(within(dialog).getByLabelText("管理员"), "a-chief");
  await userEvent.type(within(dialog).getByLabelText("名字"), "Launch");
  await userEvent.click(create);
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(team.calls.filter((c) => c.method === "createGroup").at(-1)!.args[0]).toMatchObject({
    name: "Launch", members: ["a-chief", "a-1"], admin: "a-chief", mode: "admin",
  });
  expect(await screen.findByText("Launch")).toBeTruthy();
});

it("demoting the last main agent is refused in words, and the dialog stays open", async () => {
  const { hub } = await seeded();
  render(<TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />);
  await userEvent.click(await screen.findByRole("button", { name: "Chief 的设置" }));
  const dialog = await screen.findByRole("dialog", { name: "Agent 设置" });
  await userEvent.click(within(dialog).getByRole("checkbox", { name: /主 Agent/ }));
  await userEvent.click(within(dialog).getByRole("button", { name: "保存" }));
  expect(await within(dialog).findByRole("alert")).toHaveProperty("textContent", "团队至少需要一个主 Agent");
});

it("a read-only team is shown but offers no writes", async () => {
  const { hub, team } = await seeded();
  const base = (await team.view())!;
  vi.spyOn(team, "view").mockResolvedValue({ ...base, readOnly: true });
  render(<TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />);
  expect(await screen.findByText("团队正在另一个 Reasonix 窗口中编辑，这里只能查看")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "新建 Agent" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Ada 的设置" })).toBeNull();
});

it("a kernel without a team leaves the block out", async () => {
  const hub = new MockHub();
  vi.spyOn(hub.team(), "view").mockResolvedValue(null);
  const { container } = render(<TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />);
  await act(async () => {});
  expect(container.innerHTML).toBe("");
});

it("an agent's settings say what its conversations have spent", async () => {
  const { hub, team } = await seeded();
  const base = (await team.view())!;
  const withUsage = { ...base, agents: base.agents.map((a) => (a.id === "a-1" ? { ...a, usage: { promptTokens: 12000, completionTokens: 3000, cacheHitTokens: 9000, requests: 7 } } : a)) };
  vi.spyOn(team, "view").mockResolvedValue(withUsage);
  render(<TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />);
  await userEvent.click(await screen.findByRole("button", { name: "Ada 的设置" }));
  const dialog = await screen.findByRole("dialog", { name: "Agent 设置" });
  expect(within(dialog).getByText(/用量：输入 12,000 · 输出 3,000 tokens · 缓存命中 75%/)).toBeTruthy();
});
