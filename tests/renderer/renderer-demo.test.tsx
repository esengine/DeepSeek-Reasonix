// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { describe, expect, it } from "vitest";
import {
  CharPool,
  HyperlinkPool,
  type KeystrokeSource,
  StylePool,
  mount,
} from "../../src/renderer/index.js";
import { makeTestWriter } from "../../src/renderer/runtime/test-writer.js";

import { useState } from "react";
// Local copy of the demo component so we exercise the same React tree the CLI mounts,
// without spinning up a real stdout/TTY.
import { Box, Text, useKeystroke } from "../../src/renderer/index.js";

function describeKey(k: { input: string; escape: boolean; ctrl: boolean }): string {
  if (k.escape) return "ESC";
  if (k.ctrl && k.input) return `Ctrl+${k.input.toUpperCase()}`;
  return k.input || "?";
}

function Demo({ onExit }: { onExit: () => void }) {
  const [count, setCount] = useState(0);
  const [last, setLast] = useState("(none yet)");
  useKeystroke((k) => {
    if (k.escape) {
      onExit();
      return;
    }
    setCount((n) => n + 1);
    setLast(describeKey(k));
  });
  return (
    <Box flexDirection="column" padding={1}>
      <Text>Reasonix demo</Text>
      <Box flexDirection="row">
        <Text>{"Count: "}</Text>
        <Text>{String(count)}</Text>
      </Box>
      <Box flexDirection="row">
        <Text>{"Last:  "}</Text>
        <Text>{last}</Text>
      </Box>
    </Box>
  );
}

function pools() {
  return { char: new CharPool(), style: new StylePool(), hyperlink: new HyperlinkPool() };
}

function flush(): Promise<void> {
  return new Promise((r) => setTimeout(r, 0));
}

function makeFakeStdin(): KeystrokeSource & { push: (s: string) => void } {
  let listener: ((chunk: string | Buffer) => void) | null = null;
  return {
    on(_event, cb) {
      listener = cb;
    },
    off(_event, _cb) {
      listener = null;
    },
    setRawMode() {},
    resume() {},
    pause() {},
    push(chunk: string) {
      listener?.(chunk);
    },
  };
}

describe("renderer-demo (component-level)", () => {
  it("paints initial frame with Count: 0 and the placeholder Last", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    const handle = mount(<Demo onExit={() => {}} />, {
      viewportWidth: 30,
      viewportHeight: 6,
      pools: pools(),
      write: w.write,
      stdin,
    });
    await flush();
    const out = w.output();
    expect(out).toContain("Count:");
    expect(out).toContain("0");
    expect(out).toContain("(none yet)");
    handle.destroy();
  });

  it("a printable keystroke increments count and updates last", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    const handle = mount(<Demo onExit={() => {}} />, {
      viewportWidth: 30,
      viewportHeight: 6,
      pools: pools(),
      write: w.write,
      stdin,
    });
    await flush();
    w.flush();
    stdin.push("k");
    await flush();
    const out = w.output();
    expect(out).toContain("1");
    expect(out).toContain("k");
    handle.destroy();
  });

  it("ESC triggers the onExit callback", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    let exited = false;
    const handle = mount(
      <Demo
        onExit={() => {
          exited = true;
        }}
      />,
      {
        viewportWidth: 30,
        viewportHeight: 6,
        pools: pools(),
        write: w.write,
        stdin,
      },
    );
    await flush();
    stdin.push("\x1b");
    await flush();
    expect(exited).toBe(true);
    handle.destroy();
  });
});
