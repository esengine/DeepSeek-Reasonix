// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import "./testkit";
import { Pane } from "./Pane";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";
import type { WireEvent } from "../port/wire";
import type { RuntimeView } from "../port/hub";

afterEach(cleanup);

const rt = { id: "p1", root: "/w", name: "w" } as RuntimeView;

function open() {
  const port = new MockPort();
  let emit: (ev: WireEvent) => void = () => {};
  const subscribe = port.subscribe.bind(port);
  vi.spyOn(port, "subscribe").mockImplementation((onEvent, onGap, bootstrap) => {
    emit = onEvent;
    return subscribe(onEvent, onGap, bootstrap);
  });
  const changes = vi.spyOn(port, "changes");
  render(
    <Pane port={port as AgentPort} rt={rt} title="w" active visible sideHost={null} side={false}
      onFocus={() => {}} onReport={() => {}} onSessionChanged={() => {}} pulse={0} findPulse={0}
      onSettings={() => {}} needsProject={false} onOpenProject={() => {}} onKeepHere={() => {}}
      theme="dark" dockW={560} dockMax={880} onDockW={() => {}} />,
  );
  const send = (ev: object) => act(() => emit(ev as WireEvent));
  return { changes, send };
}

describe("the explorer during a turn", () => {
  it("re-reads the working tree when a call that may write returns, not when a read does", () => {
    const pane = open();
    pane.send({ kind: "turn_started" });
    pane.changes.mockClear();

    pane.send({ kind: "tool_dispatch", tool: { id: "r1", name: "read_file", readOnly: true } });
    pane.send({ kind: "tool_result", tool: { id: "r1", name: "read_file", readOnly: true, output: "ok" } });
    expect(pane.changes).not.toHaveBeenCalled();

    pane.send({ kind: "tool_dispatch", tool: { id: "w1", name: "write_file", readOnly: false } });
    pane.send({ kind: "tool_result", tool: { id: "w1", name: "write_file", readOnly: false, output: "ok" } });
    expect(pane.changes).toHaveBeenCalledTimes(1);
  });
});
