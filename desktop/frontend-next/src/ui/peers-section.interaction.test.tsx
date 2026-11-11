// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { PeersSection } from "./PeersSection";
import { MockPort } from "../port/mock";
import { boot, STORAGE } from "../i18n";
import type { AgentPort, PeersSettings } from "../port/port";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const base: PeersSettings = {
  enabled: false, dispatch: false, release_projects: [{ path: "/work/site", name: "site" }], max_dispatch_depth: 1, max_dispatched_sessions: 8, open_nested: false,
  path: "/home/me/.reasonix/config.toml", applies: "next_turn", pending: false, available: true,
};

function draw(initial: PeersSettings = base) {
  const port = new MockPort();
  let current = initial;
  vi.spyOn(port, "peersSettings").mockImplementation(async () => current);
  vi.spyOn(port, "savePeersSettings").mockImplementation(async (edit) => {
    current = { ...current, ...(edit.enabled === undefined ? {} : { enabled: edit.enabled }), pending: true };
    return current;
  });
  const hub = { tree: vi.fn(async () => [{ root: "/work/api", name: "api", remembered: true, sessions: [] }]) };
  render(<PeersSection port={port as unknown as AgentPort} hub={hub} />);
}

it("turning session messages on unlocks the projects block without reopening the page", async () => {
  draw();
  await screen.findByText("先启用会话间消息，再放行项目。");
  expect(screen.queryByLabelText("放行这个项目")).toBeNull();
  await userEvent.click(screen.getByRole("switch", { name: "启用会话间消息" }));
  expect(await screen.findByLabelText("放行这个项目")).toBeTruthy();
  expect(screen.queryByText("先启用会话间消息，再放行项目。")).toBeNull();
});

it("turning them off locks the projects block again", async () => {
  draw({ ...base, enabled: true });
  await screen.findByLabelText("放行这个项目");
  await userEvent.click(screen.getByRole("switch", { name: "启用会话间消息" }));
  await waitFor(() => expect(screen.queryByLabelText("放行这个项目")).toBeNull());
  expect(screen.getByText("先启用会话间消息，再放行项目。")).toBeTruthy();
});
