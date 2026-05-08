import { render } from "ink";
import React from "react";
import { describe, expect, it } from "vitest";
import { CheckpointPicker, type CheckpointPickerOutcome } from "../src/cli/ui/CheckpointPicker.js";
import type {
  PickerBroadcastPorts,
  PickerSnapshot,
} from "../src/cli/ui/dashboard/use-picker-broadcast.js";
import type { CheckpointMeta } from "../src/code/checkpoints.js";
import type { DashboardEvent, PickerResolution } from "../src/server/context.js";
import { makeFakeStdin, makeFakeStdout } from "./helpers/ink-stdio.js";

function fakeCheckpoint(id: string, name: string, source: CheckpointMeta["source"] = "manual") {
  return {
    id,
    name,
    createdAt: Date.now() - 60_000,
    source,
    fileCount: 3,
    bytes: 4096,
  } satisfies CheckpointMeta;
}

function makePorts() {
  const events: DashboardEvent[] = [];
  const resolverRef = { current: null as ((res: PickerResolution) => void) | null };
  const snapshotRef = { current: null as PickerSnapshot | null };
  const ports: PickerBroadcastPorts = {
    broadcast: (ev) => events.push(ev),
    resolverRef,
    snapshotRef,
  };
  return { events, ports, fire: (res: PickerResolution) => resolverRef.current?.(res) };
}

function mount(
  checkpoints: CheckpointMeta[],
  ports: PickerBroadcastPorts,
  onChoose: (o: CheckpointPickerOutcome) => void,
) {
  const stdout = makeFakeStdout();
  return render(
    React.createElement(CheckpointPicker, {
      checkpoints,
      workspace: "/repo",
      onChoose,
      pickerPorts: ports,
    }),
    { stdout: stdout as never, stdin: makeFakeStdin() as never },
  );
}

describe("CheckpointPicker — dashboard broadcast", () => {
  it("emits modal-up with checkpoints mapped into picker items", () => {
    const { events, ports } = makePorts();
    const { unmount } = mount(
      [
        fakeCheckpoint("abcdef0123", "before-refactor"),
        fakeCheckpoint("123456789a", "auto-snap", "auto"),
      ],
      ports,
      () => undefined,
    );
    const up = events.find((e) => e.kind === "modal-up");
    if (!up || up.kind !== "modal-up" || up.modal.kind !== "picker") {
      throw new Error("expected picker modal-up");
    }
    expect(up.modal.pickerKind).toBe("checkpoints");
    expect(up.modal.items.map((i) => i.id)).toEqual(["abcdef0123", "123456789a"]);
    expect(up.modal.items[0]!.title).toBe("before-refactor");
    expect(up.modal.items[1]!.title).toBe("auto-snap (auto)");
    expect(up.modal.items[0]!.badge).toBe("abcdef0");
    expect(up.modal.actions).toEqual(["pick", "delete", "cancel"]);
    unmount();
  });

  it("translates web pick / delete / cancel into matching outcomes", () => {
    const outcomes: CheckpointPickerOutcome[] = [];
    const { ports, fire } = makePorts();
    const { unmount } = mount(
      [fakeCheckpoint("idA", "alpha"), fakeCheckpoint("idB", "beta")],
      ports,
      (o) => outcomes.push(o),
    );
    fire({ action: "pick", id: "idA" });
    fire({ action: "delete", id: "idB" });
    fire({ action: "cancel" });
    expect(outcomes).toEqual([
      { kind: "restore", id: "idA" },
      { kind: "delete", id: "idB" },
      { kind: "quit" },
    ]);
    unmount();
  });

  it("does not broadcast when pickerPorts is omitted", () => {
    const stdout = makeFakeStdout();
    const { unmount } = render(
      React.createElement(CheckpointPicker, {
        checkpoints: [fakeCheckpoint("x", "y")],
        workspace: "/repo",
        onChoose: () => undefined,
      }),
      { stdout: stdout as never, stdin: makeFakeStdin() as never },
    );
    unmount();
  });
});
