// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import "../testkit";
import { App } from "../App";
import { MockHub } from "../../port/mock_hub";
import type { RuntimeView } from "../../port/hub";

afterEach(cleanup);

// With no project yet, an ordinary pane asks for one. An agent's pane does
// not: the host gave the agent its folder, so there is nothing to choose.
function hubShowing(rt: RuntimeView) {
  const hub = new MockHub();
  hub.remoteHosts = async () => [];
  hub.tree = async () => [];
  hub.runtimes = async () => [rt];
  const build = hub.portFor.bind(hub);
  hub.portFor = (view) => {
    const port = build(view);
    port.providerSetup = async () => null;
    port.welcomeSeen = async () => true;
    return port;
  };
  return hub;
}

it("an agent's pane greets as the agent instead of asking for a project", async () => {
  const hub = hubShowing({ id: "r2", base: "/rt/r2", root: "/home/u/Reasonix Team/a-chief", name: "a-chief", team: { agent: "a-chief" } });
  await hub.team().bootstrap("Chief", "首席助理");
  render(<App hub={hub} />);
  expect(await screen.findByText(/你好，我是 Chief。/)).toBeTruthy();
  expect(screen.queryByText("先打开一个项目")).toBeNull();
  expect(screen.queryByText("描述任务，其余交给 Reasonix")).toBeNull();
});

it("an ordinary pane still asks for one", async () => {
  render(<App hub={hubShowing({ id: "r1", base: "/rt/r1", root: "/tmp/proj", name: "proj" })} />);
  expect(await screen.findByText("先打开一个项目")).toBeTruthy();
});
