import { render } from "ink";
import React from "react";
import { describe, expect, it } from "vitest";
import { PromptInput } from "../src/cli/ui/PromptInput.js";
import {
  type KeystrokeHandler,
  KeystrokeProvider,
  type KeystrokeReader,
  makeKeyEvent,
} from "../src/cli/ui/keystroke-context.js";
import type { KeyEvent } from "../src/cli/ui/stdin-reader.js";
import { makeFakeStdin, makeFakeStdout } from "./helpers/ink-stdio.js";

const ESC = String.fromCharCode(27);
const CURSOR_MOVE_RE = new RegExp(`${ESC}\\[\\d+;\\d+H`, "g");

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

function cursorMoves(text: string): string[] {
  return text.match(CURSOR_MOVE_RE) ?? [];
}

async function wait(ms = 0): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, ms));
}

async function feed(reader: FakeReader, ev: Partial<KeyEvent>): Promise<void> {
  reader.feed(ev);
  await wait();
}

async function feedText(reader: FakeReader, text: string): Promise<void> {
  for (const char of text) await feed(reader, { input: char });
}

function PromptHarness(): React.ReactElement {
  const [value, setValue] = React.useState("");
  return (
    <PromptInput
      value={value}
      onChange={setValue}
      onSubmit={() => undefined}
      onCursorChange={() => undefined}
    />
  );
}

function StaticPromptHarness({
  value,
  revision,
}: {
  value: string;
  revision: number;
}): React.ReactElement {
  void revision;
  return (
    <PromptInput
      value={value}
      onChange={() => undefined}
      onSubmit={() => undefined}
      onCursorChange={() => undefined}
    />
  );
}

describe("PromptInput system cursor sync", () => {
  it("does not rewrite an unchanged cursor position during parent rerenders", async () => {
    const reader = new FakeReader();
    const stdout = makeFakeStdout();
    const { rerender, unmount } = render(
      <KeystrokeProvider reader={reader}>
        <StaticPromptHarness value="ready" revision={0} />
      </KeystrokeProvider>,
      { stdout: stdout as never, stdin: makeFakeStdin() as never },
    );
    await wait(180);
    const before = cursorMoves(stdout.text()).length;

    rerender(
      <KeystrokeProvider reader={reader}>
        <StaticPromptHarness value="ready" revision={1} />
      </KeystrokeProvider>,
    );
    await wait(180);

    expect(cursorMoves(stdout.text()).length).toBe(before);
    unmount();
  });

  it("coalesces rapid ASCII typing into one cursor move after input goes idle", async () => {
    const reader = new FakeReader();
    const stdout = makeFakeStdout();
    const { unmount } = render(
      <KeystrokeProvider reader={reader}>
        <PromptHarness />
      </KeystrokeProvider>,
      { stdout: stdout as never, stdin: makeFakeStdin() as never },
    );
    await wait(180);

    const before = cursorMoves(stdout.text()).length;
    await feedText(reader, "11111");
    await wait(180);

    const afterMoves = cursorMoves(stdout.text());
    const newMoves = afterMoves.slice(before);
    expect(newMoves).toEqual(["\x1b[28;9H"]);

    unmount();
  });
});
