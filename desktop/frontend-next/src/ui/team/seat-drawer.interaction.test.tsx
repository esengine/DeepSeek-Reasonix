// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../testkit";
import { boot, STORAGE } from "../../i18n";
import { MockHub } from "../../port/mock_hub";
import type { RuntimeView } from "../../port/hub";
import { closeGroupStage, openGroupStage } from "../../state/teamStage";
import { closeSeatDrawer } from "../../state/teamDrawer";
import { Chrome } from "../Chrome";
import { TeamLayer } from "./TeamLayer";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
  HTMLDialogElement.prototype.showModal = function () { this.open = true; };
  HTMLDialogElement.prototype.close = function () { this.open = false; };
});
afterEach(() => {
  cleanup();
  closeGroupStage();
  closeSeatDrawer();
  vi.restoreAllMocks();
});

const ROOT = "/home/u/Reasonix Team/a-1";

async function window_(seat: RuntimeView["team"] = { agent: "a-1" }) {
  const hub = new MockHub();
  const team = hub.team();
  await team.bootstrap("Chief");
  await team.createAgent({ name: "Ada", role: "写手", instructions: "Write plainly.\nNo jargon." });
  await team.updateAgent("a-1", { permission: "ask" });
  team.addExchange({ id: "x-1", from: "a-chief", to: "a-1", digest: "d", preview: "draft the launch post", replyTo: "me", state: "replied", openedAt: "2026-10-26T08:00:00Z", at: "2026-10-26T08:05:00Z" });
  team.addExchange({ id: "x-2", from: "a-1", to: "a-chief", digest: "d", preview: "which audience?", replyTo: "me", state: "delivered", openedAt: "2026-10-26T08:01:00Z", at: "2026-10-26T08:01:00Z" });
  const adopt = vi.fn<(rt: RuntimeView) => void>();
  render(
    <>
      <Chrome port={null} workspace={null} status={{ preset: "balanced", toolApprovalMode: "ask", workspaceRoot: ROOT, sessionPath: "/s.jsonl" } as never}
        title="会话" steer={0} account={null} theme="dark" onTheme={vi.fn()} onFind={vi.fn()}
        view="flow" rows={0} surfaces={0} onView={vi.fn()} hub={hub} seat={seat} onError={vi.fn()} />
      <TeamLayer hub={hub} active="r2" seat={seat} root={ROOT} onAdoptPane={async (rt) => adopt(await rt)} />
    </>,
  );
  return { hub, team, adopt };
}

const opened = () => screen.findByRole("complementary", { name: "Ada 的详情" });

it("the agent's name in the chrome opens and closes its details", async () => {
  await window_();
  const toggle = await screen.findByRole("button", { name: "Ada" });
  await userEvent.click(toggle);
  const drawer = await opened();
  expect(toggle.getAttribute("aria-expanded")).toBe("true");
  expect(within(drawer).getByText("写手")).toBeTruthy();
  expect(within(drawer).getByText(ROOT)).toBeTruthy();
  expect(within(drawer).getByText(/Write plainly\./)).toBeTruthy();
  await userEvent.click(toggle);
  await waitFor(() => expect(screen.queryByRole("complementary")).toBeNull());
});

it("the permission changes on the spot", async () => {
  const { team } = await window_();
  await userEvent.click(await screen.findByRole("button", { name: "Ada" }));
  const drawer = await opened();
  await userEvent.selectOptions(within(drawer).getByLabelText("权限"), "auto");
  expect(team.calls.at(-1)).toEqual({ method: "updateAgent", args: ["a-1", { permission: "auto" }] });
});

it("each exchange says who, what and how it stands, and opens the other side", async () => {
  const { adopt } = await window_();
  await userEvent.click(await screen.findByRole("button", { name: "Ada" }));
  const drawer = await opened();
  const list = within(drawer).getByRole("list", { name: "往来" });
  const rows = await within(list).findAllByRole("button");
  expect(rows).toHaveLength(2);
  expect(rows[0].textContent).toContain("来自 Chief");
  expect(rows[0].textContent).toContain("draft the launch post");
  expect(rows[0].textContent).toContain("已回复");
  expect(rows[1].textContent).toContain("发给 Chief");
  expect(rows[1].textContent).toContain("which audience?");
  expect(rows[1].textContent).toContain("进行中");
  await userEvent.click(rows[0]);
  await waitFor(() => expect(adopt).toHaveBeenCalledWith(expect.objectContaining({ team: { agent: "a-chief" } })));
});

it("the settings dialog opens from the details", async () => {
  await window_();
  await userEvent.click(await screen.findByRole("button", { name: "Ada" }));
  await userEvent.click(within(await opened()).getByRole("button", { name: "Ada 的设置" }));
  expect(await screen.findByRole("dialog", { name: "Agent 设置" })).toBeTruthy();
});

it("a group's chat takes the area, and the details step aside", async () => {
  await window_();
  await userEvent.click(await screen.findByRole("button", { name: "Ada" }));
  await opened();
  act(() => openGroupStage("g-none"));
  await waitFor(() => expect(screen.queryByRole("complementary")).toBeNull());
});

it("a new folder is recorded now and read when the agent next opens", async () => {
  const { hub, team } = await window_();
  vi.spyOn(hub, "pickFolder").mockResolvedValue("/work/site");
  await userEvent.click(await screen.findByRole("button", { name: "Ada" }));
  const drawer = await opened();
  await userEvent.click(within(drawer).getByRole("button", { name: "更改…" }));
  await waitFor(() => expect(team.calls.at(-1)).toEqual({ method: "updateAgent", args: ["a-1", { folder: "/work/site" }] }));
  expect(await within(drawer).findByText("下次打开这个 Agent 时生效")).toBeTruthy();
});
