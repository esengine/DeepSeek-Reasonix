import { render } from "ink";
import React from "react";
import { describe, expect, it } from "vitest";
import { SessionPicker, type SessionPickerOutcome } from "../src/cli/ui/SessionPicker.js";
import type {
  PickerBroadcastPorts,
  PickerSnapshot,
} from "../src/cli/ui/dashboard/use-picker-broadcast.js";
import type { SessionInfo } from "../src/memory/session.js";
import type { DashboardEvent, PickerResolution } from "../src/server/context.js";
import { makeFakeStdin, makeFakeStdout } from "./helpers/ink-stdio.js";

function fakeSession(name: string, branch = "main"): SessionInfo {
  return {
    name,
    path: `/tmp/${name}.jsonl`,
    size: 100,
    messageCount: 4,
    mtime: new Date("2026-05-06T00:00:00Z"),
    meta: { branch, summary: `${name} session`, totalCostUsd: 0.1, turnCount: 2, workspace: "/r" },
  };
}

function makePorts(): {
  ports: PickerBroadcastPorts;
  events: DashboardEvent[];
  fire: (res: PickerResolution) => void;
} {
  const events: DashboardEvent[] = [];
  const resolverRef = { current: null as ((res: PickerResolution) => void) | null };
  const snapshotRef = { current: null as PickerSnapshot | null };
  return {
    events,
    ports: {
      broadcast: (ev) => {
        events.push(ev);
      },
      resolverRef,
      snapshotRef,
    },
    fire: (res) => resolverRef.current?.(res),
  };
}

function mount(
  sessions: SessionInfo[],
  ports: PickerBroadcastPorts,
  onChoose: (o: SessionPickerOutcome) => void,
) {
  const stdout = makeFakeStdout();
  return render(
    React.createElement(SessionPicker, {
      sessions,
      workspace: "/repo",
      onChoose,
      pickerPorts: ports,
    }),
    { stdout: stdout as never, stdin: makeFakeStdin() as never },
  );
}

describe("SessionPicker — dashboard broadcast", () => {
  it("emits modal-up with sessions mapped into picker items", () => {
    const { events, ports } = makePorts();
    const { unmount } = mount(
      [fakeSession("alpha"), fakeSession("beta", "feature/x")],
      ports,
      () => undefined,
    );
    const up = events.find((e) => e.kind === "modal-up");
    expect(up).toBeTruthy();
    if (!up || up.kind !== "modal-up" || up.modal.kind !== "picker") {
      throw new Error("expected picker modal-up");
    }
    expect(up.modal.pickerKind).toBe("sessions");
    expect(up.modal.items.map((i) => i.id)).toEqual(["alpha", "beta"]);
    expect(up.modal.items[1]!.badge).toBe("feature/x");
    expect(up.modal.actions).toEqual(["pick", "delete", "rename", "new", "cancel"]);
    unmount();
  });

  it("emits modal-down on unmount", () => {
    const { events, ports } = makePorts();
    const { unmount } = mount([fakeSession("a")], ports, () => undefined);
    unmount();
    const down = events.find((e) => e.kind === "modal-down");
    expect(down).toBeTruthy();
    if (!down || down.kind !== "modal-down") throw new Error();
    expect(down.modalKind).toBe("picker");
  });

  it("translates web pick into onChoose open", () => {
    const outcomes: SessionPickerOutcome[] = [];
    const { ports, fire } = makePorts();
    const { unmount } = mount([fakeSession("alpha")], ports, (o) => outcomes.push(o));
    fire({ action: "pick", id: "alpha" });
    expect(outcomes).toEqual([{ kind: "open", name: "alpha" }]);
    unmount();
  });

  it("translates web delete / rename / new / cancel into matching outcomes", () => {
    const outcomes: SessionPickerOutcome[] = [];
    const { ports, fire } = makePorts();
    const { unmount } = mount([fakeSession("alpha"), fakeSession("beta")], ports, (o) =>
      outcomes.push(o),
    );
    fire({ action: "delete", id: "alpha" });
    fire({ action: "rename", id: "beta", text: "beta-renamed" });
    fire({ action: "new" });
    fire({ action: "cancel" });
    expect(outcomes).toEqual([
      { kind: "delete", name: "alpha" },
      { kind: "rename", name: "beta", newName: "beta-renamed" },
      { kind: "new" },
      { kind: "quit" },
    ]);
    unmount();
  });

  it("does not broadcast when pickerPorts is omitted", () => {
    const stdout = makeFakeStdout();
    const { unmount } = render(
      React.createElement(SessionPicker, {
        sessions: [fakeSession("a")],
        workspace: "/repo",
        onChoose: () => undefined,
      }),
      { stdout: stdout as never, stdin: makeFakeStdin() as never },
    );
    unmount();
  });
});
