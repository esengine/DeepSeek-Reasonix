// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../testkit";
import { boot, STORAGE } from "../../i18n";
import { MockHub } from "../../port/mock_hub";
import type { RuntimeView } from "../../port/hub";
import { TeamRail } from "./TeamRail";
import { AgentHero } from "./AgentHero";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const RT: RuntimeView = { id: "r2", base: "/rt/r2", root: "/team/a-chief", name: "a-chief" };

async function draw(seat: RuntimeView["team"], current?: string) {
  const hub = new MockHub();
  const team = hub.team();
  await team.bootstrap("Chief", "首席助理");
  await team.createAgent({ name: "Ada", role: "写手" });
  await team.createGroup({ name: "Launch", members: ["a-chief", "a-1"] });
  const port = hub.portFor(RT);
  const setModel = vi.spyOn(port, "setModel");
  const starter = vi.fn();
  const changed = vi.fn();
  render(
    <>
      <TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />
      <AgentHero port={port} seat={seat!} model={current} onModel={changed} onStarter={starter} />
    </>,
  );
  return { team, port, setModel, starter, changed };
}

it("the main agent greets, offers the models, and keeps the one picked", async () => {
  const { team, setModel, changed } = await draw({ agent: "a-chief" });
  const hero = await screen.findByRole("region", { name: "Chief" });
  expect(within(hero).getByText(/你好，我是 Chief/)).toBeTruthy();
  const models = within(hero).getByRole("group", { name: "为 Chief 选一个模型" });
  const chips = await within(models).findAllByRole("button");
  expect(chips.length).toBeGreaterThan(0);
  const ref = chips[0].dataset.value!;
  await userEvent.click(chips[0]);
  await waitFor(() => expect(setModel).toHaveBeenCalledWith(ref));
  expect(team.calls.at(-1)).toEqual({ method: "updateAgent", args: ["a-chief", { model: ref }] });
  expect(changed).toHaveBeenCalled();
});

it("the chip of the model already in use reads as chosen", async () => {
  const hub = new MockHub();
  const refs = (await hub.portFor(RT).models("chat")).map((m) => m.ref);
  await draw({ agent: "a-chief" }, refs[1]);
  const models = await screen.findByRole("group", { name: "为 Chief 选一个模型" });
  await waitFor(() => expect(within(models).getAllByRole("button", { pressed: true }).map((b) => b.dataset.value)).toEqual([refs[1]]));
});

it("the main agent's starters fill the composer rather than sending", async () => {
  const { starter } = await draw({ agent: "a-chief" });
  const hero = await screen.findByRole("region", { name: "Chief" });
  await userEvent.click(within(hero).getByRole("button", { name: "介绍一下你能做什么" }));
  expect(starter).toHaveBeenCalledWith("介绍一下你能做什么");
});

it("another agent greets by its role and offers no starters", async () => {
  await draw({ agent: "a-1" });
  const hero = await screen.findByRole("region", { name: "Ada" });
  expect(within(hero).getByText("你好，我是 Ada，写手。有什么需要我做的？")).toBeTruthy();
  expect(within(hero).queryByRole("button", { name: "介绍一下你能做什么" })).toBeNull();
});

it("a member's group conversation says what it is for", async () => {
  await draw({ agent: "a-1", group: "g-2" });
  const hero = await screen.findByRole("region", { name: "Ada" });
  expect(within(hero).getByText("这是 Ada 在「Launch」里的对话：群里轮到它发言时，消息会出现在这里。")).toBeTruthy();
  expect(within(hero).queryByRole("group", { name: /选一个模型/ })).toBeNull();
});
