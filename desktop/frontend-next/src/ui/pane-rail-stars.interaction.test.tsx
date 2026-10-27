// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import "./testkit";
import { Pane } from "./Pane";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";
import type { RuntimeView } from "../port/hub";

afterEach(cleanup);

const port = new MockPort();
port.history = async () => [
  { role: "user", content: "第一个问题", msgIndex: 0 },
  { role: "assistant", content: "第一段回答", msgIndex: 1 },
  { role: "user", content: "第二个问题", msgIndex: 2 },
  { role: "assistant", content: "第二段回答", msgIndex: 3 },
];

const pane = (rt: RuntimeView, port: AgentPort) => (
  <Pane port={port} rt={rt} title="w" active visible sideHost={null} side={false}
    onFocus={() => {}} onReport={() => {}} onSessionChanged={() => {}} pulse={0} findPulse={0}
    onSettings={() => {}} needsProject={false} onOpenProject={() => {}} onKeepHere={() => {}}
    theme="dark" dockW={560} dockMax={880} onDockW={() => {}} />
);

const firstMark = async () => (await screen.findAllByRole("button", { name: /定位到你说的/ }))[0];

it.each(["full", "paged"])("a question starred before the first save stays with the saved conversation using %s history", async (mode) => {
  localStorage.clear();
  const reading: AgentPort = Object.assign(new MockPort(), { history: port.history });
  if (mode === "paged") reading.historyPage = async () => ({
    messages: await port.history(), toolContext: [], snapshot: "stars", start: 0, end: 4, total: 4, hasMoreBefore: false,
  });
  const unsaved = { id: "r1", root: "/w", name: "w" } as RuntimeView;
  const view = render(pane(unsaved, reading));
  fireEvent.contextMenu(await firstMark());
  await waitFor(async () => expect((await firstMark()).getAttribute("data-starred")).toBe(""));

  view.rerender(pane({ ...unsaved, sessionPath: "/w/.reasonix/sessions/a.jsonl" }, reading));
  await waitFor(async () => expect((await firstMark()).getAttribute("data-starred")).toBe(""));
  // Runtime ids restart at r1 every launch, so a star kept under one would
  // reappear in whichever unsaved conversation that pane holds next.
  expect(Object.keys(localStorage).filter((k) => k.endsWith(":r1"))).toEqual([]);
});
