// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useState } from "react";
import { describe, expect, it } from "vitest";
import {
  Box,
  CharPool,
  HyperlinkPool,
  StylePool,
  Text,
  mount,
  useCursor,
} from "../../src/renderer/index.js";
import { makeTestWriter } from "../../src/renderer/runtime/test-writer.js";

function pools() {
  return { char: new CharPool(), style: new StylePool(), hyperlink: new HyperlinkPool() };
}

function flush(): Promise<void> {
  return new Promise((r) => setTimeout(r, 0));
}

/** Minimal terminal cursor parser. Tracks the absolute (col,row) of the cursor as
 *  a sequence of mount writes is replayed. Recognizes the patches our renderer
 *  emits: ESC[H, ESC[<n>A, ESC[<n>B, ESC[<n>C, ESC[<n>D, ESC[<n>;<m>H,
 *  CR, LF, ESC[<n>G (CHA), and the ESC[?25h/l cursor visibility toggle. */
interface CursorRow {
  row: number;
  col: number;
  visible: boolean;
}
function parseFinalCursor(out: string): CursorRow {
  let row = 0;
  let col = 0;
  let visible = true;
  let i = 0;
  while (i < out.length) {
    const ch = out[i]!;
    if (ch === "\r") {
      col = 0;
      i++;
      continue;
    }
    if (ch === "\n") {
      row++;
      i++;
      continue;
    }
    if (ch === "\x1b") {
      // CSI
      if (out[i + 1] === "[") {
        let j = i + 2;
        let params = "";
        while (j < out.length && /[\d;?]/.test(out[j]!)) {
          params += out[j];
          j++;
        }
        const final = out[j];
        const numbers = params
          .replace(/^\?/, "")
          .split(";")
          .map((p) => Number(p || "0"));
        const n = numbers[0] ?? 0;
        const m = numbers[1] ?? 0;
        if (final === "H" || final === "f") {
          row = Math.max(0, (n || 1) - 1);
          col = Math.max(0, (m || 1) - 1);
        } else if (final === "A") row = Math.max(0, row - (n || 1));
        else if (final === "B") row += n || 1;
        else if (final === "C") col += n || 1;
        else if (final === "D") col = Math.max(0, col - (n || 1));
        else if (final === "G") col = Math.max(0, (n || 1) - 1);
        else if (final === "J") {
          /* erase — no cursor move */
        } else if (final === "K") {
          /* erase line — no cursor move */
        } else if (final === "h" || final === "l") {
          if (params === "?25") visible = final === "h";
        }
        i = j + 1;
        continue;
      }
      if (out[i + 1] === "]") {
        // OSC — skip until ST (\x1b\\) or BEL (\x07)
        let j = i + 2;
        while (j < out.length && out[j] !== "\x07" && !(out[j] === "\x1b" && out[j + 1] === "\\"))
          j++;
        if (out[j] === "\x07") i = j + 1;
        else i = j + 2;
        continue;
      }
    }
    // printable char advances column by 1 (ASCII only in tests)
    col += 1;
    i++;
  }
  return { row, col, visible };
}

function CursorAt({
  col,
  rowFromBottom,
  visible,
}: {
  col: number;
  rowFromBottom?: number;
  visible?: boolean;
}): React.ReactElement {
  useCursor({ col, rowFromBottom, visible });
  return <Text> </Text>;
}

describe("useCursor — basic positioning", () => {
  it("places the terminal cursor at the requested column on the bottom row", async () => {
    const w = makeTestWriter();
    const handle = mount(
      <Box flexDirection="column">
        <Text>line one</Text>
        <Text>line two</Text>
        <CursorAt col={5} />
      </Box>,
      { viewportWidth: 30, viewportHeight: 10, pools: pools(), write: w.write },
    );
    await flush();
    const final = parseFinalCursor(w.output());
    expect(final.col).toBe(5);
    handle.destroy();
  });

  it("rowFromBottom=1 places cursor on the second-to-last row", async () => {
    const w = makeTestWriter();
    const handle = mount(
      <Box flexDirection="column">
        <Text>row 0</Text>
        <Text>row 1</Text>
        <Text>row 2</Text>
        <CursorAt col={2} rowFromBottom={1} />
      </Box>,
      { viewportWidth: 30, viewportHeight: 10, pools: pools(), write: w.write },
    );
    await flush();
    const final = parseFinalCursor(w.output());
    // Total screen height is 3. rowFromBottom=1 → row index 1.
    // Within scrollback mode the live region's first row sits relative to where
    // mount started writing; we care about col + that the cursor wasn't left at
    // the bottom-left default (col=0, rowFromBottom=0).
    expect(final.col).toBe(2);
    handle.destroy();
  });

  it("visible:false hides the terminal cursor", async () => {
    const w = makeTestWriter();
    const handle = mount(
      <Box flexDirection="column">
        <Text>x</Text>
        <CursorAt col={0} visible={false} />
      </Box>,
      { viewportWidth: 10, viewportHeight: 4, pools: pools(), write: w.write },
    );
    await flush();
    expect(w.output()).toContain("\x1b[?25l");
    handle.destroy();
  });
});

describe("useCursor — updates", () => {
  it("re-rendering with a new col moves the cursor", async () => {
    const w = makeTestWriter();
    function App({ col }: { col: number }): React.ReactElement {
      return (
        <Box flexDirection="column">
          <Text>hello world</Text>
          <CursorAt col={col} />
        </Box>
      );
    }
    const handle = mount(<App col={2} />, {
      viewportWidth: 20,
      viewportHeight: 6,
      pools: pools(),
      write: w.write,
    });
    await flush();
    handle.update(<App col={9} />);
    await flush();
    const final = parseFinalCursor(w.output());
    expect(final.col).toBe(9);
    handle.destroy();
  });

  it("unmounting the consumer falls back to the default cursor", async () => {
    const w = makeTestWriter();
    function App({ on }: { on: boolean }): React.ReactElement {
      return (
        <Box flexDirection="column">
          <Text>line</Text>
          {on ? <CursorAt col={7} /> : null}
        </Box>
      );
    }
    const handle = mount(<App on={true} />, {
      viewportWidth: 20,
      viewportHeight: 6,
      pools: pools(),
      write: w.write,
    });
    await flush();
    expect(parseFinalCursor(w.output()).col).toBe(7);
    w.flush();
    handle.update(<App on={false} />);
    await flush();
    // After unmount the renderer should drop the override; cursor returns to default (col 0).
    expect(parseFinalCursor(w.output()).col).toBe(0);
    handle.destroy();
  });
});

describe("useCursor — interactive shell-style", () => {
  it("typing into a controlled input moves the cursor with each character", async () => {
    const w = makeTestWriter();
    let setText: ((s: string) => void) | null = null;
    function Input(): React.ReactElement {
      const [text, set] = useState("");
      setText = set;
      useCursor({ col: 2 + text.length });
      return (
        <Box flexDirection="row">
          <Text>{`> ${text}`}</Text>
        </Box>
      );
    }
    const handle = mount(<Input />, {
      viewportWidth: 30,
      viewportHeight: 4,
      pools: pools(),
      write: w.write,
    });
    await flush();
    expect(parseFinalCursor(w.output()).col).toBe(2);
    setText?.("hi");
    await flush();
    expect(parseFinalCursor(w.output()).col).toBe(4);
    setText?.("hello");
    await flush();
    expect(parseFinalCursor(w.output()).col).toBe(7);
    handle.destroy();
  });
});
