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
import { SeatAgent } from "./seat";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const call: Tool = { id: "c1", name: "request_secret", args: JSON.stringify({ name: "shop key", purpose: "to call the shop API" }), output: "Asked the user for SHOP_KEY." } as Tool;

async function asked(seat = "a-1", setup?: (team: ReturnType<MockHub["team"]>) => Promise<void> | void) {
  const hub = new MockHub();
  const team = hub.team();
  await team.bootstrap("Chief");
  await team.createAgent({ name: "Ada" });
  await team.createAgent({ name: "Ben" });
  await setup?.(team);
  team.askSecret("SHOP_KEY", "to call the shop API", "a-1");
  render(
    <>
      <TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />
      <SeatAgent.Provider value={seat}>
        <SecretCard tool={call} running={false} />
      </SeatAgent.Provider>
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
  expect((within(card).getByRole("radio", { name: "只给 Ada" }) as HTMLInputElement).checked).toBe(true);
  await userEvent.type(input, "sk-shop-0123456789");
  await userEvent.click(within(card).getByRole("button", { name: "保存" }));
  await waitFor(() => expect(team.calls.at(-1)?.method).toBe("saveSecret"));
  expect(team.calls.at(-1)?.args[0]).toEqual({ name: "SHOP_KEY", value: "sk-shop-0123456789", purpose: "to call the shop API", agents: ["a-1"], requester: "a-1" });
  expect(await within(card).findByText("已保存 SHOP_KEY · 只给 Ada")).toBeTruthy();
  expect(document.body.innerHTML).not.toContain("sk-shop-0123456789");
});

it("a card answers only its own agent's request", async () => {
  await asked("a-2", (team) => team.askSecret("SHOP_KEY", "Ben's reason", "a-2"));
  const card = await screen.findByRole("group", { name: "Ben 需要 SHOP_KEY" });
  expect(within(card).getByText("Ben's reason")).toBeTruthy();
  expect(screen.queryByRole("group", { name: "Ada 需要 SHOP_KEY" })).toBeNull();
});

it("answering one agent keeps the secret with the agents it was already given to", async () => {
  const { team } = await asked("a-1", (team) => team.saveSecret({ name: "SHOP_KEY", value: "sk-old-0123456789", agents: ["a-2"] }));
  const card = await screen.findByRole("group", { name: "Ada 需要 SHOP_KEY" });
  expect(within(card).getByText("已经保存过 SHOP_KEY，保存会替换它的值")).toBeTruthy();
  expect((within(card).getByRole("radio", { name: "只给 Ben、Ada" }) as HTMLInputElement).checked).toBe(true);
  await userEvent.type(within(card).getByLabelText("密钥"), "sk-new-0123456789");
  await userEvent.click(within(card).getByRole("button", { name: "保存" }));
  await waitFor(() => expect(team.calls.at(-1)?.method).toBe("saveSecret"));
  expect(team.calls.at(-1)?.args[0]).toMatchObject({ agents: ["a-2", "a-1"], requester: "a-1" });
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
  expect(screen.getByText("删除后命令不再收到它；值会留在本机，只用来继续遮住已经出现过它的对话。")).toBeTruthy();
  await userEvent.click(within(list).getByRole("button", { name: "确认删除 SHOP_KEY" }));
  expect(team.calls.at(-1)).toEqual({ method: "deleteSecret", args: ["SHOP_KEY"] });
  expect(await screen.findByText("已停用、仍在遮住：SHOP_KEY")).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: "清除停用的值" }));
  expect(team.calls.at(-1)?.method).toBe("deleteSecret");
  await userEvent.click(screen.getByRole("button", { name: "确认清除停用的值" }));
  await waitFor(() => expect(team.calls.at(-1)).toEqual({ method: "forgetRetiredSecrets", args: [] }));
  await waitFor(() => expect(screen.queryByText(/已停用、仍在遮住/)).toBeNull());
});

it("on a host that does not keep agents out of the file, the card says any agent can read it", async () => {
  await asked("a-1", (team) => team.unguardSecrets());
  const card = await screen.findByRole("group", { name: "Ada 需要 SHOP_KEY" });
  expect(within(card).getByRole("alert").textContent).toBe("这台机器上，Agent 的命令读得到存机密的文件（Windows，或沙箱没有开启）：保存在这里的值，任何 Agent 都可能读到。");
});

it("on a host that does, the card raises no such warning", async () => {
  await asked();
  const card = await screen.findByRole("group", { name: "Ada 需要 SHOP_KEY" });
  expect(within(card).queryByRole("alert")).toBeNull();
});
