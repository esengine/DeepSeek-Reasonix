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

function applyBytes(bytes: string, width: number, height: number): string[] {
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
      cy = Math.min(height - 1, cy + 1);
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
      else if (final === "H") {
        cy = 0;
        cx = 0;
      } else if (final === "J") {
        for (let y = cy; y < height; y++) {
          for (let x = y === cy ? cx : 0; x < width; x++) rows[y]![x] = " ";
        }
      }
      i = j + 1;
      continue;
    }
    if (ch === "\x1b" && bytes[i + 1] === "]") {
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
  return rows.map((r) => r.join("").trimEnd());
}

function Tower({ rows }: { rows: number }) {
  return (
    <Box flexDirection="column">
      {Array.from({ length: rows }).map((_, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: ordered list, stable per render
        <Text key={`row-${i}`}>{`row-${i}`}</Text>
      ))}
    </Box>
  );
}

describe("virtual viewport — bottom-stick + PgUp/PgDn scrolling", () => {
  it("layout taller than viewport: window pinned at bottom by default", async () => {
    const w = makeTestWriter();
    const handle = mount(<Tower rows={20} />, {
      viewportWidth: 20,
      viewportHeight: 6,
      pools: pools(),
      write: w.write,
      scroll: "virtual",
    });
    await flush();
    const screen = applyBytes(w.output(), 20, 6);
    expect(screen.slice(0, 5)).toEqual(["row-15", "row-16", "row-17", "row-18", "row-19"]);
    handle.destroy();
  });

  it("PgUp scrolls back; older rows become visible", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    const handle = mount(<Tower rows={20} />, {
      viewportWidth: 20,
      viewportHeight: 6,
      pools: pools(),
      write: w.write,
      stdin,
      scroll: "virtual",
    });
    await flush();
    stdin.push("\x1b[5~");
    await flush();
    const screen = applyBytes(w.output(), 20, 6);
    expect(screen.slice(0, 5)).toEqual(["row-10", "row-11", "row-12", "row-13", "row-14"]);
    handle.destroy();
  });

  it("Home scrolls all the way to the top", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    const handle = mount(<Tower rows={30} />, {
      viewportWidth: 20,
      viewportHeight: 6,
      pools: pools(),
      write: w.write,
      stdin,
      scroll: "virtual",
    });
    await flush();
    stdin.push("\x1b[H");
    await flush();
    const screen = applyBytes(w.output(), 20, 6);
    expect(screen.slice(0, 5)).toEqual(["row-0", "row-1", "row-2", "row-3", "row-4"]);
    handle.destroy();
  });

  it("wheel-up scrolls back by ~3 rows; wheel-down by ~3 rows", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    const handle = mount(<Tower rows={20} />, {
      viewportWidth: 20,
      viewportHeight: 6,
      pools: pools(),
      write: w.write,
      stdin,
      scroll: "virtual",
    });
    await flush();
    stdin.push("\x1b[<64;1;1M");
    await flush();
    let screen = applyBytes(w.output(), 20, 6);
    expect(screen.slice(0, 5)).toEqual(["row-12", "row-13", "row-14", "row-15", "row-16"]);
    stdin.push("\x1b[<65;1;1M");
    await flush();
    screen = applyBytes(w.output(), 20, 6);
    expect(screen.slice(0, 5)).toEqual(["row-15", "row-16", "row-17", "row-18", "row-19"]);
    handle.destroy();
  });

  it("enters alt-screen + mouse capture at mount; restores on destroy", async () => {
    const w = makeTestWriter();
    const handle = mount(<Tower rows={5} />, {
      viewportWidth: 20,
      viewportHeight: 6,
      pools: pools(),
      write: w.write,
      stdin: makeFakeStdin(),
      scroll: "virtual",
    });
    await flush();
    const onMount = w.output();
    expect(onMount).toContain("\x1b[?1049h");
    expect(onMount).toContain("\x1b[?1002h");
    expect(onMount).toContain("\x1b[?1006h");
    handle.destroy();
    const afterDestroy = w.output();
    expect(afterDestroy).toContain("\x1b[?1049l");
    expect(afterDestroy).toContain("\x1b[?1002l");
    expect(afterDestroy).toContain("\x1b[?1006l");
  });

  it("End scrolls back to the bottom", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    const handle = mount(<Tower rows={30} />, {
      viewportWidth: 20,
      viewportHeight: 6,
      pools: pools(),
      write: w.write,
      stdin,
      scroll: "virtual",
    });
    await flush();
    stdin.push("\x1b[H");
    await flush();
    stdin.push("\x1b[F");
    await flush();
    const screen = applyBytes(w.output(), 20, 6);
    expect(screen.slice(0, 5)).toEqual(["row-25", "row-26", "row-27", "row-28", "row-29"]);
    handle.destroy();
  });

  it("layout grows while sticky-bottom: viewport keeps showing latest", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    let bumpRows: () => void = () => {};
    function App() {
      const [n, setN] = useState(10);
      bumpRows = () => setN((x) => x + 5);
      return <Tower rows={n} />;
    }
    const handle = mount(<App />, {
      viewportWidth: 20,
      viewportHeight: 6,
      pools: pools(),
      write: w.write,
      stdin,
      scroll: "virtual",
    });
    await flush();
    bumpRows();
    await flush();
    const screen = applyBytes(w.output(), 20, 6);
    expect(screen.slice(0, 5)).toEqual(["row-10", "row-11", "row-12", "row-13", "row-14"]);
    handle.destroy();
  });

  it("after user scrolls up, layout grow does not steal the scroll position", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    let bumpRows: () => void = () => {};
    function App() {
      const [n, setN] = useState(20);
      bumpRows = () => setN((x) => x + 3);
      return <Tower rows={n} />;
    }
    const handle = mount(<App />, {
      viewportWidth: 20,
      viewportHeight: 6,
      pools: pools(),
      write: w.write,
      stdin,
      scroll: "virtual",
    });
    await flush();
    stdin.push("\x1b[H");
    await flush();
    bumpRows();
    await flush();
    const screen = applyBytes(w.output(), 20, 6);
    expect(screen.slice(0, 5)).toEqual(["row-0", "row-1", "row-2", "row-3", "row-4"]);
    handle.destroy();
  });
});
