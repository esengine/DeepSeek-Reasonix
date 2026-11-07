// @vitest-environment jsdom
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import "./testkit";
import { Pane } from "./Pane";
import { MockPort } from "../port/mock";
import type { RuntimeView } from "../port/hub";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it("drops the goal card once its end button has cleared the goal, with no turn running to refresh the status", async () => {
  const port = new MockPort();
  let goal = "看看项目优化点";
  const status = port.status.bind(port);
  vi.spyOn(port, "status").mockImplementation(async () => ({ ...(await status()), goal, goalStatus: goal ? "stopped" : "" }));
  const submit = vi.spyOn(port, "submit").mockImplementation(async (text) => { if (text === "/goal clear") goal = ""; });
  render(<Pane port={port} rt={{ id: "p1", root: "/w", name: "w" } as RuntimeView}
    title="w" active visible sideHost={null} side={false} onFocus={() => {}} onReport={() => {}}
    onSessionChanged={() => {}} pulse={0} findPulse={0} onSettings={() => {}} needsProject={false}
    onOpenProject={() => {}} onKeepHere={() => {}} theme="dark" dockW={560} dockMax={880} onDockW={() => {}} />);
  await userEvent.click(await screen.findByRole("button", { name: /目标/, expanded: false }));
  await userEvent.click(await screen.findByRole("button", { name: "退出目标" }));
  expect(submit).toHaveBeenCalledWith("/goal clear");
  await waitFor(() => expect(screen.queryByRole("button", { name: "退出目标" })).toBeNull());
});
