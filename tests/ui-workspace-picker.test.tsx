import { render } from "ink";
import React from "react";
import { describe, expect, it } from "vitest";
import { WorkspacePicker, type WorkspacePickerOutcome } from "../src/cli/ui/WorkspacePicker.js";
import {
  type KeystrokeHandler,
  KeystrokeProvider,
  type KeystrokeReader,
  makeKeyEvent,
} from "../src/cli/ui/keystroke-context.js";
import type { KeyEvent } from "../src/cli/ui/stdin-reader.js";
import type { WorkspaceInfo } from "../src/workspaces.js";
import { makeFakeStdin, makeFakeStdout } from "./helpers/ink-stdio.js";

class FakeReader implements KeystrokeReader {
  private readonly handlers = new Set<KeystrokeHandler>();

  start(): void {
    // no-op
  }

  subscribe(handler: KeystrokeHandler): () => void {
    this.handlers.add(handler);
    return () => {
      this.handlers.delete(handler);
    };
  }

  feed(ev: Partial<KeyEvent>): void {
    const event = makeKeyEvent(ev);
    for (const handler of [...this.handlers]) handler(event);
  }
}

function workspace(path: string, sessions = 0): WorkspaceInfo {
  return {
    path,
    current: false,
    sessions,
    lastActive: sessions > 0 ? new Date("2026-05-06T00:00:00Z") : undefined,
  };
}

function mount(reader: FakeReader, onChoose: (o: WorkspacePickerOutcome) => void) {
  const stdout = makeFakeStdout();
  return render(
    <KeystrokeProvider reader={reader}>
      <WorkspacePicker
        workspaces={[workspace("/repo/main", 2), workspace("/repo/release", 1)]}
        currentWorkspace="/repo/main"
        onChoose={onChoose}
      />
    </KeystrokeProvider>,
    { stdout: stdout as never, stdin: makeFakeStdin() as never },
  );
}

async function flush(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0));
}

async function feed(reader: FakeReader, ev: Partial<KeyEvent>): Promise<void> {
  reader.feed(ev);
  await flush();
}

async function feedText(reader: FakeReader, text: string): Promise<void> {
  for (const char of text) await feed(reader, { input: char });
}

describe("WorkspacePicker", () => {
  it("filters by path and opens the matching workspace", async () => {
    const reader = new FakeReader();
    const outcomes: WorkspacePickerOutcome[] = [];
    const { unmount } = mount(reader, (o) => outcomes.push(o));
    await flush();

    await feed(reader, { input: "/" });
    await feedText(reader, "release");
    await feed(reader, { return: true });

    expect(outcomes).toEqual([{ kind: "open", path: "/repo/release" }]);
    unmount();
  });
});
