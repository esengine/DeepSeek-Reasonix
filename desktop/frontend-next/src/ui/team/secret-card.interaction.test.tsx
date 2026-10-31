// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../testkit";
import { boot, STORAGE } from "../../i18n";
import { MockHub } from "../../port/mock_hub";
import type { Tool } from "../../port/wire";
import { TeamRail } from "./TeamRail";
import { TeamSection } from "./TeamSection";
import { SecretCard } from "./SecretCard";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const call: Tool = { id: "c1", name: "request_secret", args: JSON.stringify({ name: "shop key", purpose: "to call the shop API" }), output: "Asked the user for SHOP_KEY." } as Tool;

async function asked() {
  const hub = new MockHub();
  const team = hub.team();
  await team.bootstrap("Chief");
  await team.createAgent({ name: "Ada" });
  team.askSecret("SHOP_KEY", "to call the shop API", "a-1");
  render(
    <>
      <TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />
      <SecretCard tool={call} running={false} />
    </>,
  );
  return { hub, team };
}

it("the agent's request becomes a card the person fills in, and the value never comes back", async () => {
  const { team } = await asked();
  const card = await screen.findByRole("group", { name: "Ada 需要 SHOP_KEY" });
  expect(within(card).getByText("to call the shop API")).toBeTruthy();
  const input = within(card).getByLabelText("密钥");
  expect(input.getAttribute("type")).toBe("password");
  await userEvent.type(input, "sk-shop-0123456789");
  expect((within(card).getByRole("radio", { name: "只给 Ada" }) as HTMLInputElement).checked).toBe(true);
  await userEvent.click(within(card).getByRole("button", { name: "保存" }));
  await waitFor(() => expect(team.calls.at(-1)?.method).toBe("saveSecret"));
  expect(team.calls.at(-1)?.args[0]).toEqual({ name: "SHOP_KEY", value: "sk-shop-0123456789", purpose: "to call the shop API", agents: ["a-1"], requester: "a-1" });
  expect(await within(card).findByText("已保存 SHOP_KEY · 只给 Ada")).toBeTruthy();
  expect(document.body.innerHTML).not.toContain("sk-shop-0123456789");
});

it("declining tells the agent, and the card says so", async () => {
  const { team } = await asked();
  const card = await screen.findByRole("group", { name: "Ada 需要 SHOP_KEY" });
  await userEvent.click(within(card).getByRole("button", { name: "不提供" }));
  await waitFor(() => expect(team.calls.at(-1)).toEqual({ method: "cancelSecret", args: ["SHOP_KEY", "a-1"] }));
  expect(await within(card).findByText("已拒绝提供 SHOP_KEY")).toBeTruthy();
});

it("settings list what was saved, never its value, and remove it with two presses", async () => {
  const hub = new MockHub();
  const team = hub.team();
  await team.bootstrap("Chief");
  await team.saveSecret({ name: "SHOP_KEY", value: "sk-shop-0123456789", purpose: "the shop", agents:["a-chief"] });
  render(<TeamSection hub={hub} />);
  const list = await screen.findByRole("list", { name: "机密" });
  expect(within(list).getByText("SHOP_KEY")).toBeTruthy();
  expect(within(list).getByText(/只给 Chief/)).toBeTruthy();
  expect(document.body.innerHTML).not.toContain("sk-shop");
  await userEvent.click(within(list).getByRole("button", { name: "删除 SHOP_KEY" }));
  await userEvent.click(within(list).getByRole("button", { name: "确认删除 SHOP_KEY" }));
  expect(team.calls.at(-1)).toEqual({ method: "deleteSecret", args: ["SHOP_KEY"] });
});
