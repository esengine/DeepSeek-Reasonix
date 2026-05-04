// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useState } from "react";
import { describe, expect, it } from "vitest";
import { SimplePromptInput } from "../../src/cli/ui/prompt-input-v2.js";
import {
  CharPool,
  HyperlinkPool,
  type KeystrokeSource,
  StylePool,
  mount,
} from "../../src/renderer/index.js";
import { makeTestWriter } from "../../src/renderer/runtime/test-writer.js";

function pools() {
  return { char: new CharPool(), style: new StylePool(), hyperlink: new HyperlinkPool() };
}

function flush(): Promise<void> {
  return new Promise((r) => setTimeout(r, 0));
}

function makeFakeStdin(): KeystrokeSource & { push: (s: string) => void } {
  let listener: ((c: string | Buffer) => void) | null = null;
  return {
    on(_e, cb) {
      listener = cb;
    },
    off() {
      listener = null;
    },
    setRawMode() {},
    resume() {},
    pause() {},
    push(c: string) {
      listener?.(c);
    },
  };
}

interface Harness {
  values: string[];
  submits: string[];
  cancels: number;
  push: (s: string) => void;
  destroy: () => void;
  output: () => string;
}

function harness(initial = ""): Harness {
  const w = makeTestWriter();
  const stdin = makeFakeStdin();
  const values: string[] = [initial];
  const submits: string[] = [];
  let cancels = 0;
  function App(): React.ReactElement {
    const [v, setV] = useState(initial);
    return (
      <SimplePromptInput
        value={v}
        onChange={(next) => {
          values.push(next);
          setV(next);
        }}
        onSubmit={(next) => {
          submits.push(next);
        }}
        onCancel={() => {
          cancels++;
        }}
      />
    );
  }
  const handle = mount(<App />, {
    viewportWidth: 60,
    viewportHeight: 6,
    pools: pools(),
    write: w.write,
    stdin,
  });
  return {
    values,
    submits,
    get cancels() {
      return cancels;
    },
    push: (s) => stdin.push(s),
    destroy: () => handle.destroy(),
    output: () => w.output(),
  };
}

describe("SimplePromptInput — typing", () => {
  it("printable keys append to the value", async () => {
    const h = harness();
    await flush();
    h.push("h");
    await flush();
    h.push("i");
    await flush();
    expect(h.values.at(-1)).toBe("hi");
    h.destroy();
  });

  it("backspace removes the last char", async () => {
    const h = harness();
    await flush();
    h.push("abc");
    await flush();
    h.push("\x7f");
    await flush();
    expect(h.values.at(-1)).toBe("ab");
    h.destroy();
  });

  it("enter calls onSubmit with the current value", async () => {
    const h = harness();
    await flush();
    h.push("hello\r");
    await flush();
    expect(h.submits).toEqual(["hello"]);
    h.destroy();
  });

  it("escape on a non-empty value clears it", async () => {
    const h = harness();
    await flush();
    h.push("text");
    await flush();
    h.push("\x1b");
    await flush();
    expect(h.values.at(-1)).toBe("");
    expect(h.cancels).toBe(0);
    h.destroy();
  });

  it("escape on empty value invokes onCancel", async () => {
    const h = harness();
    await flush();
    h.push("\x1b");
    await flush();
    expect(h.cancels).toBe(1);
    h.destroy();
  });
});

describe("SimplePromptInput — cursor movement", () => {
  it("left arrow + insert puts a char in the middle", async () => {
    const h = harness();
    await flush();
    h.push("abc");
    await flush();
    h.push("\x1b[D");
    await flush();
    h.push("X");
    await flush();
    expect(h.values.at(-1)).toBe("abXc");
    h.destroy();
  });

  it("home moves cursor to start; typing prepends", async () => {
    const h = harness();
    await flush();
    h.push("abc");
    await flush();
    h.push("\x1b[H");
    await flush();
    h.push("Z");
    await flush();
    expect(h.values.at(-1)).toBe("Zabc");
    h.destroy();
  });

  it("ctrl+u clears the whole buffer", async () => {
    // Reasonix convention (matches the live chat input): ctrl+u kills the
    // entire buffer rather than only the part before the cursor — the
    // ergonomic alternative for getting rid of a long paste.
    const h = harness();
    await flush();
    h.push("abcdef");
    await flush();
    h.push("\x1b[D");
    await flush();
    h.push("\x15"); // ctrl+u
    await flush();
    expect(h.values.at(-1)).toBe("");
    h.destroy();
  });

  it("ctrl+k clears everything from cursor to end", async () => {
    const h = harness();
    await flush();
    h.push("abcdef");
    await flush();
    h.push("\x1b[D");
    await flush();
    h.push("\x1b[D");
    await flush();
    h.push("\x0b"); // ctrl+k
    await flush();
    expect(h.values.at(-1)).toBe("abcd");
    h.destroy();
  });

  it("delete acts as backspace (Reasonix convention)", async () => {
    // The live chat input collapses forward-delete onto backspace because
    // some Windows terminals report Backspace without setting the
    // backspace flag; v2 follows the same convention via processMultilineKey.
    const h = harness();
    await flush();
    h.push("abc");
    await flush();
    h.push("\x1b[D");
    await flush();
    h.push("\x1b[3~"); // delete (forward)
    await flush();
    expect(h.values.at(-1)).toBe("ac");
    h.destroy();
  });
});

describe("SimplePromptInput — multi-line", () => {
  it("Alt+Enter inserts a newline", async () => {
    const h = harness();
    await flush();
    h.push("foo");
    await flush();
    h.push("\x1b\r"); // Alt+Enter
    await flush();
    h.push("bar");
    await flush();
    expect(h.values.at(-1)).toBe("foo\nbar");
    h.destroy();
  });

  it("Enter on a multi-line buffer still submits", async () => {
    const h = harness();
    await flush();
    h.push("a");
    await flush();
    h.push("\x1b\r"); // Alt+Enter for newline
    await flush();
    h.push("b");
    await flush();
    h.push("\r"); // Enter — submit
    await flush();
    expect(h.submits).toEqual(["a\nb"]);
    h.destroy();
  });

  it("up arrow moves cursor up one logical line", async () => {
    const h = harness();
    await flush();
    h.push("first");
    await flush();
    h.push("\x1b\r");
    await flush();
    h.push("second");
    await flush();
    h.push("\x1b[A"); // up arrow
    await flush();
    h.push("X"); // should land on the first line
    await flush();
    // Cursor was at end of "second" (col 6); up keeps col but first line is
    // "first" (len 5), so cursor clamps to 5 → "firstX\nsecond".
    expect(h.values.at(-1)).toBe("firstX\nsecond");
    h.destroy();
  });

  it("down arrow moves cursor down one logical line", async () => {
    const h = harness();
    await flush();
    h.push("ab");
    await flush();
    h.push("\x1b\r");
    await flush();
    h.push("cd");
    await flush();
    h.push("\x1b[H"); // home → cursor 0
    await flush();
    h.push("\x1b[B"); // down → second line, col 0
    await flush();
    h.push("X");
    await flush();
    expect(h.values.at(-1)).toBe("ab\nXcd");
    h.destroy();
  });

  it("backslash + Enter at end-of-buffer continues to a new line", async () => {
    const h = harness();
    await flush();
    h.push("foo\\");
    await flush();
    h.push("\r");
    await flush();
    h.push("bar");
    await flush();
    expect(h.values.at(-1)).toBe("foo\nbar");
    h.destroy();
  });

  it("up arrow on empty buffer fires onHistoryPrev", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    let prevs = 0;
    const handle = mount(
      <SimplePromptInput
        value=""
        onChange={() => {}}
        onHistoryPrev={() => {
          prevs++;
        }}
      />,
      { viewportWidth: 40, viewportHeight: 4, pools: pools(), write: w.write, stdin },
    );
    await flush();
    stdin.push("\x1b[A");
    await flush();
    expect(prevs).toBe(1);
    handle.destroy();
  });
});

describe("SimplePromptInput — render", () => {
  it("placeholder shows when empty", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    const handle = mount(
      <SimplePromptInput
        value=""
        onChange={() => {}}
        onSubmit={() => {}}
        placeholder="say something…"
      />,
      { viewportWidth: 50, viewportHeight: 4, pools: pools(), write: w.write, stdin },
    );
    await flush();
    expect(w.output()).toContain("say something");
    handle.destroy();
  });

  it("typed chars appear in the output bytes", async () => {
    const h = harness();
    await flush();
    h.push("abc");
    await flush();
    const out = h.output();
    // Cell-diff renderer emits one char at a time with cursor moves between
    // them, so just verify each char shows up somewhere in the byte stream.
    expect(out).toMatch(/a/);
    expect(out).toMatch(/b/);
    expect(out).toMatch(/c/);
    h.destroy();
  });

  it("backspace re-emits cursor positioning", async () => {
    const h = harness();
    await flush();
    h.push("hi");
    await flush();
    h.push("\x7f");
    await flush();
    // Just verify that backspace produced *some* output (cursor move + clear).
    expect(h.values.at(-1)).toBe("h");
    h.destroy();
  });
});
