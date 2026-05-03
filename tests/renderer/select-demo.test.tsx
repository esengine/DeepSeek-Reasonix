// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useState } from "react";
import { describe, expect, it } from "vitest";
import {
  CharPool,
  HyperlinkPool,
  type KeystrokeSource,
  StylePool,
  inkCompat,
  mount,
  useKeystroke,
} from "../../src/renderer/index.js";
import { makeTestWriter } from "../../src/renderer/runtime/test-writer.js";

interface Item {
  readonly value: string;
  readonly label: string;
  readonly disabled?: boolean;
}

const ITEMS: ReadonlyArray<Item> = [
  { value: "a", label: "Apple" },
  { value: "b", label: "Banana", disabled: true },
  { value: "c", label: "Cherry" },
];

function findEnabled(items: ReadonlyArray<Item>, from: number, step: -1 | 1): number {
  let i = from;
  for (let tries = 0; tries < items.length; tries++) {
    i = (i + step + items.length) % items.length;
    if (!items[i]?.disabled) return i;
  }
  return from;
}

function Select({
  onSubmit,
  onCancel,
}: {
  onSubmit: (v: string) => void;
  onCancel: () => void;
}): React.ReactElement {
  const [index, setIndex] = useState(0);
  useKeystroke((k) => {
    if (k.upArrow) setIndex((i) => findEnabled(ITEMS, i, -1));
    else if (k.downArrow) setIndex((i) => findEnabled(ITEMS, i, +1));
    else if (k.return) {
      const item = ITEMS[index];
      if (item && !item.disabled) onSubmit(item.value);
    } else if (k.escape) onCancel();
  });
  return (
    <inkCompat.Box flexDirection="column">
      {ITEMS.map((item, i) => (
        <inkCompat.Text
          key={item.value}
          color={item.disabled ? "gray" : i === index ? "cyan" : undefined}
          dimColor={item.disabled}
        >
          {`${i === index ? "▸" : " "} ${item.label}`}
        </inkCompat.Text>
      ))}
    </inkCompat.Box>
  );
}

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

describe("select-demo — end-to-end", () => {
  it("renders all items with the first row marked active", async () => {
    const w = makeTestWriter();
    const handle = mount(<Select onSubmit={() => {}} onCancel={() => {}} />, {
      viewportWidth: 30,
      viewportHeight: 5,
      pools: pools(),
      write: w.write,
      stdin: makeFakeStdin(),
    });
    await flush();
    const out = w.output();
    expect(out).toContain("Apple");
    expect(out).toContain("Banana");
    expect(out).toContain("Cherry");
    expect(out).toContain("▸");
    handle.destroy();
  });

  it("ArrowDown skips disabled items", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    let picked: string | null = null;
    const handle = mount(
      <Select
        onSubmit={(v) => {
          picked = v;
        }}
        onCancel={() => {}}
      />,
      {
        viewportWidth: 30,
        viewportHeight: 5,
        pools: pools(),
        write: w.write,
        stdin,
      },
    );
    await flush();
    stdin.push("\x1b[B");
    await flush();
    stdin.push("\r");
    await flush();
    expect(picked).toBe("c");
    handle.destroy();
  });

  it("Enter on the initial active row submits", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    let picked: string | null = null;
    const handle = mount(
      <Select
        onSubmit={(v) => {
          picked = v;
        }}
        onCancel={() => {}}
      />,
      {
        viewportWidth: 30,
        viewportHeight: 5,
        pools: pools(),
        write: w.write,
        stdin,
      },
    );
    await flush();
    stdin.push("\r");
    await flush();
    expect(picked).toBe("a");
    handle.destroy();
  });

  it("ESC triggers onCancel", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    let cancelled = false;
    const handle = mount(
      <Select
        onSubmit={() => {}}
        onCancel={() => {
          cancelled = true;
        }}
      />,
      {
        viewportWidth: 30,
        viewportHeight: 5,
        pools: pools(),
        write: w.write,
        stdin,
      },
    );
    await flush();
    stdin.push("\x1b");
    await flush();
    expect(cancelled).toBe(true);
    handle.destroy();
  });

  it("ArrowUp from the first enabled wraps to the last enabled", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    let picked: string | null = null;
    const handle = mount(
      <Select
        onSubmit={(v) => {
          picked = v;
        }}
        onCancel={() => {}}
      />,
      {
        viewportWidth: 30,
        viewportHeight: 5,
        pools: pools(),
        write: w.write,
        stdin,
      },
    );
    await flush();
    stdin.push("\x1b[A");
    await flush();
    stdin.push("\r");
    await flush();
    expect(picked).toBe("c");
    handle.destroy();
  });
});
