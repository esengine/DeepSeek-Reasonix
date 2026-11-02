// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../testkit";
import { boot, STORAGE } from "../../i18n";
import { MockHub } from "../../port/mock_hub";
import { setTeamMotion } from "../../state/teamMotion";
import { TeamRail } from "./TeamRail";
import { TeamSection } from "./TeamSection";

beforeEach(() => {
  localStorage.clear();
  localStorage.setItem(STORAGE, "zh");
  boot();
  setTeamMotion("normal");
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

async function seeded() {
  const hub = new MockHub();
  const team = hub.team();
  await team.bootstrap("Chief");
  await team.createAgent({ name: "Ada", role: "写手" });
  await team.createAgent({ name: "Ben", role: "程序员" });
  return { hub, team };
}

it("the agents' motion is chosen here, held on this machine, and read by the page", async () => {
  const { hub } = await seeded();
  render(<TeamSection hub={hub} />);
  const motion = await screen.findByRole("group", { name: "Agent 动效" });
  expect(within(motion).getByRole("button", { name: "正常" }).getAttribute("aria-pressed")).toBe("true");
  await userEvent.click(within(motion).getByRole("button", { name: "活泼" }));
  expect(document.documentElement.dataset.teamMotion).toBe("lively");
  expect(localStorage.getItem("rx-team-motion")).toBe("lively");
});

it("what was hidden from the sidebar is listed here and can be shown again", async () => {
  const { hub, team } = await seeded();
  await team.updateAgent("a-1", { hidden: true });
  render(<TeamSection hub={hub} />);
  const hidden = await screen.findByRole("list", { name: "已隐藏" });
  await userEvent.click(within(hidden).getByRole("button", { name: "在侧栏显示 Ada" }));
  expect(team.calls.at(-1)).toEqual({ method: "updateAgent", args: ["a-1", { hidden: false }] });
  await waitFor(() => expect(screen.getByText("没有隐藏的 Agent 或群聊")).toBeTruthy());
});

// jsdom lays nothing out, so a row's box is read from where it sits in its list.
function layOut() {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
    const at = this.parentElement ? [...this.parentElement.children].indexOf(this) : 0;
    return { top: at * 40, bottom: at * 40 + 40, left: 0, right: 200, width: 200, height: 40, x: 0, y: at * 40, toJSON: () => ({}) } as DOMRect;
  });
  const animate = vi.fn(() => ({ cancel() {} }) as unknown as Animation);
  Object.defineProperty(HTMLElement.prototype, "animate", { value: animate, configurable: true, writable: true });
  return animate;
}

it("a row that changes place slides there, and nothing moves on mount", async () => {
  const animate = layOut();
  const { hub, team } = await seeded();
  render(<TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />);
  await screen.findByText("Ben");
  expect(animate).not.toHaveBeenCalled();
  act(() => team.touch("a-2", 1000));
  act(() => team.touch("a-1", 2000));
  await waitFor(() => expect(animate).toHaveBeenCalled());
});

it("quiet motion leaves the reorder still", async () => {
  const animate = layOut();
  setTeamMotion("quiet");
  const { hub, team } = await seeded();
  render(<TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />);
  await screen.findByText("Ben");
  act(() => team.touch("a-2", 1000));
  act(() => team.touch("a-1", 2000));
  await act(async () => {});
  expect(animate).not.toHaveBeenCalled();
});
