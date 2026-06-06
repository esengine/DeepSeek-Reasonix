import { render } from "ink";
import React from "react";
import { describe, expect, it, vi } from "vitest";
import { PromptInput } from "../src/cli/ui/PromptInput.js";
import {
  type KeystrokeHandler,
  KeystrokeProvider,
  type KeystrokeReader,
  makeKeyEvent,
} from "../src/cli/ui/keystroke-context.js";
import type { KeyEvent } from "../src/cli/ui/stdin-reader.js";
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

async function wait(ms = 0): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, ms));
}

function PromptHarness({ onSubmit }: { onSubmit: (value: string) => void }): React.ReactElement {
  const [value, setValue] = React.useState("");
  return <PromptInput value={value} onChange={setValue} onSubmit={onSubmit} />;
}

function renderPrompt(onSubmit: (value: string) => void) {
  const reader = new FakeReader();
  const stdout = makeFakeStdout();
  const instance = render(
    <KeystrokeProvider reader={reader}>
      <PromptHarness onSubmit={onSubmit} />
    </KeystrokeProvider>,
    {
      stdout: stdout as never,
      stdin: makeFakeStdin() as never,
      incrementalRendering: true,
    },
  );
  return { reader, ...instance };
}

describe("PromptInput IME handling", () => {
  it("does not submit when an IME commits an ASCII word immediately before Enter (#1853)", async () => {
    const onSubmit = vi.fn();
    const { reader, unmount } = renderPrompt(onSubmit);
    await wait();

    reader.feed({ input: "hello" });
    reader.feed({ return: true });
    await wait();

    expect(onSubmit).not.toHaveBeenCalled();
    unmount();
  });

  it("still submits after ordinary per-key ASCII typing", async () => {
    const onSubmit = vi.fn();
    const { reader, unmount } = renderPrompt(onSubmit);
    await wait();

    for (const ch of "hello") reader.feed({ input: ch });
    reader.feed({ return: true });
    await wait();

    expect(onSubmit).toHaveBeenCalledWith("hello");
    unmount();
  });
});
