import { render } from "ink";
import React from "react";
import { describe, expect, it } from "vitest";
import { SessionPicker, type SessionPickerOutcome } from "../src/cli/ui/SessionPicker.js";
import {
  type KeystrokeHandler,
  KeystrokeProvider,
  type KeystrokeReader,
  makeKeyEvent,
} from "../src/cli/ui/keystroke-context.js";
import type { KeyEvent } from "../src/cli/ui/stdin-reader.js";
import type { SessionInfo } from "../src/memory/session.js";
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

function fakeSession(name: string, meta: Partial<SessionInfo["meta"]>): SessionInfo {
  return {
    name,
    path: `/tmp/${name}.jsonl`,
    size: 100,
    messageCount: 4,
    mtime: new Date("2026-05-06T00:00:00Z"),
    meta: {
      branch: "main",
      summary: `${name} session`,
      totalCostUsd: 0.1,
      turnCount: 2,
      workspace: "/repo",
      ...meta,
    },
  };
}

function mount(reader: FakeReader, onChoose: (o: SessionPickerOutcome) => void) {
  const stdout = makeFakeStdout();
  return render(
    <KeystrokeProvider reader={reader}>
      <SessionPicker
        sessions={[
          fakeSession("alpha", { summary: "frontend dashboard polish" }),
          fakeSession("beta", { summary: "release packaging repair" }),
        ]}
        workspace="/repo"
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

describe("SessionPicker search", () => {
  it("filters by session summary and opens the matching session", async () => {
    const reader = new FakeReader();
    const outcomes: SessionPickerOutcome[] = [];
    const { unmount } = mount(reader, (o) => outcomes.push(o));
    await flush();

    await feed(reader, { input: "/" });
    await feedText(reader, "release");
    await feed(reader, { return: true });

    expect(outcomes).toEqual([{ kind: "open", name: "beta" }]);
    unmount();
  });

  it("clears search with Escape before quitting the picker", async () => {
    const reader = new FakeReader();
    const outcomes: SessionPickerOutcome[] = [];
    const { unmount } = mount(reader, (o) => outcomes.push(o));
    await flush();

    await feed(reader, { input: "/" });
    await feedText(reader, "release");
    await feed(reader, { escape: true });
    expect(outcomes).toEqual([]);

    await feed(reader, { escape: true });
    expect(outcomes).toEqual([{ kind: "quit" }]);
    unmount();
  });
});
