// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../testkit";
import { boot, STORAGE } from "../../i18n";
import { MockHub } from "../../port/mock_hub";
import type { RuntimeView } from "../../port/hub";
import { TeamLayer } from "./TeamLayer";
import { TeamRail } from "./TeamRail";
import { UserText } from "./UserText";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

async function draw(text: string) {
  const hub = new MockHub();
  const team = hub.team();
  await team.bootstrap("Chief");
  await team.createAgent({ name: "Ada" });
  const adopt = vi.fn<(rt: RuntimeView) => void>();
  render(
    <>
      <TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />
      <TeamLayer hub={hub} active="" onAdoptPane={async (rt) => adopt(await rt)} />
      <UserText text={text} />
    </>,
  );
  return { team, adopt };
}

it("a teammate the person mentioned opens from the message, and a quoted conversation says whose", async () => {
  const { team, adopt } = await draw("ask @Ada about @agent:a-1, mail a@b.com, and @nobody");
  const mention = await screen.findByRole("button", { name: "@Ada" });
  expect(await screen.findByText("Ada 的对话")).toBeTruthy();
  expect(screen.getByText(/mail a@b\.com, and @nobody/)).toBeTruthy();
  await userEvent.click(mention);
  await waitFor(() => expect(adopt).toHaveBeenCalledWith(expect.objectContaining({ team: { agent: "a-1" } })));
  expect(team.calls.at(-1)).toEqual({ method: "openAgent", args: ["a-1"] });
});

it("off the team a message reads as it was written", async () => {
  render(<UserText text="ask @Ada about @agent:a-1" />);
  expect(screen.getByText("ask @Ada about @agent:a-1")).toBeTruthy();
  expect(screen.queryByRole("button")).toBeNull();
});
