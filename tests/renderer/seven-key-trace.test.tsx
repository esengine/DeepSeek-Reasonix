// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useState } from "react";
import { describe, expect, it } from "vitest";
import {
  Box,
  CharPool,
  HyperlinkPool,
  type KeystrokeSource,
  StylePool,
  Text,
  mount,
  useKeystroke,
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

interface ParsedScreen {
  width: number;
  rows: string[];
  cursor: { x: number; y: number };
}

function applyBytes(bytes: string, width: number, height: number): ParsedScreen {
  const rows: string[][] = Array.from({ length: height }, () => Array(width).fill(" "));
  let cx = 0;
  let cy = 0;
  let i = 0;
  while (i < bytes.length) {
    const ch = bytes[i]!;
    if (ch === "\r") {
      cx = 0;
      i++;
      continue;
    }
    if (ch === "\n") {
      cy++;
      if (cy >= height) cy = height - 1;
      i++;
      continue;
    }
    if (ch === "\x1b" && bytes[i + 1] === "[") {
      let j = i + 2;
      let arg = "";
      while (j < bytes.length && /[0-9;]/.test(bytes[j]!)) {
        arg += bytes[j];
        j++;
      }
      const final = bytes[j];
      const n = arg.length === 0 ? 1 : Number.parseInt(arg.split(";")[0]!, 10);
      if (final === "A") cy = Math.max(0, cy - n);
      else if (final === "B") cy = Math.min(height - 1, cy + n);
      else if (final === "C") cx = Math.min(width - 1, cx + n);
      else if (final === "D") cx = Math.max(0, cx - n);
      else if (final === "G") cx = Math.max(0, n - 1);
      else if (final === "J") {
        for (let y = cy; y < height; y++) {
          for (let x = y === cy ? cx : 0; x < width; x++) rows[y]![x] = " ";
        }
      }
      // SGR (m), hyperlinks, etc. — ignored
      i = j + 1;
      continue;
    }
    if (ch === "\x1b" && bytes[i + 1] === "]") {
      // OSC sequence — skip until ST
      let j = i + 2;
      while (
        j < bytes.length &&
        bytes[j] !== "\x07" &&
        !(bytes[j] === "\x1b" && bytes[j + 1] === "\\")
      ) {
        j++;
      }
      i = bytes[j] === "\x07" ? j + 1 : j + 2;
      continue;
    }
    if (cx < width && cy < height) {
      rows[cy]![cx] = ch;
      cx++;
    } else {
      cx++;
    }
    i++;
  }
  return { width, rows: rows.map((r) => r.join("")), cursor: { x: cx, y: cy } };
}

function Demo() {
  const [count, setCount] = useState(0);
  const [last, setLast] = useState("(none yet)");
  useKeystroke((k) => {
    if (k.escape) return;
    setCount((n) => n + 1);
    setLast(k.input || "?");
  });
  return (
    <Box flexDirection="column" padding={1}>
      <Text>Reasonix demo</Text>
      <Text>Press any key.</Text>
      <Box paddingTop={1} flexDirection="row">
        <Text>{"Count : "}</Text>
        <Text>{String(count)}</Text>
      </Box>
      <Box flexDirection="row">
        <Text>{"Last  : "}</Text>
        <Text>{last}</Text>
      </Box>
    </Box>
  );
}

describe("seven keystrokes — value lands on the count row, not the padding row", () => {
  it("after 7 keystrokes, count row shows '7' and last row shows the most recent key", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    const VIEW_W = 30;
    const VIEW_H = 20;
    const handle = mount(<Demo />, {
      viewportWidth: VIEW_W,
      viewportHeight: VIEW_H,
      pools: pools(),
      write: w.write,
      stdin,
    });
    await flush();
    for (const ch of "abcdefg") {
      stdin.push(ch);
      await flush();
    }
    const screen = applyBytes(w.output(), VIEW_W, VIEW_H);
    // Layout: padTop=1 wrapper, demo box has its own border-less padding=1.
    // Inner: row 0 outer-pad, 1 "Reasonix demo", 2 "Press any key.", 3 count-padTop, 4 count row, 5 last row, 6 outer-pad.
    const countRow = screen.rows[4];
    const lastRow = screen.rows[5];
    expect(countRow).toBeDefined();
    expect(lastRow).toBeDefined();
    expect(countRow).toContain("Count : 7");
    expect(lastRow).toContain("Last  : g");
  });
});
