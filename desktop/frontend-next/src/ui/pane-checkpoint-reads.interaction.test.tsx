// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, renderHook, waitFor } from "@testing-library/react";
import "./testkit";
import { Pane } from "./Pane";
import { useCheckpointRefresh } from "./useCheckpointRefresh";
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
  const status = vi.spyOn(port, "status");
  const checkpoints = vi.spyOn(port, "checkpoints");
  render(
    <Pane port={port as AgentPort} rt={rt} title="w" active visible sideHost={null} side={false}
      onFocus={() => {}} onReport={() => {}} onSessionChanged={() => {}} pulse={0} findPulse={0}
      onSettings={() => {}} needsProject={false} onOpenProject={() => {}} onKeepHere={() => {}}
      theme="dark" dockW={560} dockMax={880} onDockW={() => {}} />,
  );
  const send = (ev: object) => act(() => emit(ev as WireEvent));
  return { status, checkpoints, send };
}

// Opening a pane reads its checkpoints once; learning the session path from the
// first status is not a reason to read them again. Both turn edges are: the
// kernel opens a turn's checkpoint when it starts and fills it while it runs.
it("reads checkpoints once on mount and again at each turn edge", async () => {
  const pane = open();
  await waitFor(() => expect(pane.status).toHaveBeenCalled());
  await act(async () => {
    await Promise.resolve();
  });
  expect(pane.checkpoints).toHaveBeenCalledTimes(1);

  pane.send({ kind: "turn_started" });
  await waitFor(() => expect(pane.checkpoints).toHaveBeenCalledTimes(2));
  pane.send({ kind: "turn_done" });
  await waitFor(() => expect(pane.checkpoints).toHaveBeenCalledTimes(3));
});

function hook(checkpoints: AgentPort["checkpoints"]) {
  const port = { checkpoints: vi.fn(checkpoints) } as unknown as AgentPort;
  const set = vi.fn();
  const r = renderHook(({ path, running }) => useCheckpointRefresh(port, path, running, set), {
    initialProps: { path: undefined as string | undefined, running: false },
  });
  return { ...r, port, set };
}

it("re-reads checkpoints for another session and at turn edges, not for the first path", () => {
  const { rerender, port } = hook(async () => []);
  expect(port.checkpoints).toHaveBeenCalledTimes(1);
  rerender({ path: "/s/a.jsonl", running: false });
  expect(port.checkpoints).toHaveBeenCalledTimes(1);
  rerender({ path: "/s/b.jsonl", running: false });
  expect(port.checkpoints).toHaveBeenCalledTimes(2);
  rerender({ path: "/s/b.jsonl", running: true });
  expect(port.checkpoints).toHaveBeenCalledTimes(3);
  rerender({ path: "/s/b.jsonl", running: false });
  expect(port.checkpoints).toHaveBeenCalledTimes(4);
});

// No later edge may come to cover a failed read, so it is tried once more.
it("tries a failed read once more", async () => {
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
  try {
    const cps = [{ turn: 1 }] as Awaited<ReturnType<AgentPort["checkpoints"]>>;
    const answers = [() => Promise.reject(new Error("offline")), () => Promise.resolve(cps), () => Promise.reject(new Error("offline"))];
    const { port, set } = hook(() => answers.shift()!());
    await act(async () => {
      await vi.runAllTimersAsync();
    });
    expect(port.checkpoints).toHaveBeenCalledTimes(2);
    expect(set).toHaveBeenCalledExactlyOnceWith(cps);
  } finally {
    vi.useRealTimers();
  }
});

it("never lets an older read overwrite a newer one", async () => {
  type List = Awaited<ReturnType<AgentPort["checkpoints"]>>;
  let resolveOlder: (cps: List) => void = () => {};
  const older = new Promise<List>((resolve) => (resolveOlder = resolve));
  const newer = [{ turn: 2 }] as List;
  const answers = [older, Promise.resolve(newer)];
  const { rerender, set } = hook(() => answers.shift()!);
  rerender({ path: undefined, running: true });
  await waitFor(() => expect(set).toHaveBeenCalledWith(newer));
  await act(async () => {
    resolveOlder([{ turn: 1 }] as List);
    await older;
  });
  expect(set).toHaveBeenCalledExactlyOnceWith(newer);
});
