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
const FULL_FRAME_ERASE_RE = new RegExp(`${ESC}\\[2K${ESC}\\[1A${ESC}\\[2K`);

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

async function wait(ms = 0): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, ms));
}

async function waitForWriteAfter(stdout: ReturnType<typeof makeFakeStdout>, before: number) {
  for (let attempt = 0; attempt < 20; attempt++) {
    if (stdout.raw().length > before) return;
    await wait(25);
  }
  expect(stdout.raw().length).toBeGreaterThan(before);
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

describe("PromptInput repaint behavior", () => {
  it("does not erase the whole frame when prompt edits render incrementally", async () => {
    const reader = new FakeReader();
    const stdout = makeFakeStdout();
    const { unmount } = render(
      <KeystrokeProvider reader={reader}>
        <PromptHarness />
      </KeystrokeProvider>,
      {
        stdout: stdout as never,
        stdin: makeFakeStdin() as never,
        incrementalRendering: true,
      },
    );
    await wait(180);

    const before = stdout.raw().length;
    reader.feed({ input: "a" });
    await waitForWriteAfter(stdout, before);
    await wait(180);

    const delta = stdout.raw().slice(before);
    expect(delta).not.toMatch(FULL_FRAME_ERASE_RE);
    expect(stdout.text()).toContain("a");

    unmount();
  });
});
