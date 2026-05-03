// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useState } from "react";
import { describe, expect, it } from "vitest";
import {
  KeystrokeReader,
  type KeystrokeSource,
  useKeystroke,
} from "../../../src/renderer/input/index.js";
import { CharPool } from "../../../src/renderer/pools/char-pool.js";
import { HyperlinkPool } from "../../../src/renderer/pools/hyperlink-pool.js";
import { StylePool } from "../../../src/renderer/pools/style-pool.js";
import { Text } from "../../../src/renderer/react/components.js";
import { mount } from "../../../src/renderer/reconciler/mount.js";
import { makeTestWriter } from "../../../src/renderer/runtime/test-writer.js";

function pools() {
  return {
    char: new CharPool(),
    style: new StylePool(),
    hyperlink: new HyperlinkPool(),
  };
}

function flush(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

function makeFakeStdin(): KeystrokeSource & {
  push: (chunk: string) => void;
  rawMode: boolean;
} {
  let listener: ((chunk: string | Buffer) => void) | null = null;
  let rawMode = false;
  return {
    on(_event, cb) {
      listener = cb;
    },
    off(_event, _cb) {
      listener = null;
    },
    setRawMode(raw: boolean) {
      rawMode = raw;
    },
    resume() {},
    pause() {},
    push(chunk: string) {
      listener?.(chunk);
    },
    get rawMode() {
      return rawMode;
    },
  };
}

describe("KeystrokeReader", () => {
  it("emits parsed keystrokes from stream chunks", () => {
    const stdin = makeFakeStdin();
    const reader = new KeystrokeReader({ source: stdin });
    const seen: string[] = [];
    reader.subscribe((k) => seen.push(k.input || (k.return ? "<RET>" : "?")));
    stdin.push("ab\r");
    expect(seen).toEqual(["a", "b", "<RET>"]);
    reader.destroy();
  });

  it("setRawMode is enabled on construction and disabled on destroy", () => {
    const stdin = makeFakeStdin();
    const reader = new KeystrokeReader({ source: stdin });
    expect(stdin.rawMode).toBe(true);
    reader.destroy();
    expect(stdin.rawMode).toBe(false);
  });

  it("subscribe returns an unsubscribe handle", () => {
    const stdin = makeFakeStdin();
    const reader = new KeystrokeReader({ source: stdin });
    const seen: string[] = [];
    const unsub = reader.subscribe((k) => seen.push(k.input));
    stdin.push("a");
    unsub();
    stdin.push("b");
    expect(seen).toEqual(["a"]);
    reader.destroy();
  });

  it("destroy stops further events from reaching listeners", () => {
    const stdin = makeFakeStdin();
    const reader = new KeystrokeReader({ source: stdin });
    let count = 0;
    reader.subscribe(() => count++);
    reader.destroy();
    stdin.push("xyz");
    expect(count).toBe(0);
  });
});

describe("useKeystroke — React hook", () => {
  it("re-renders on each key as state changes", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    function Echo() {
      const [last, setLast] = useState("");
      useKeystroke((k) => setLast(k.input || (k.escape ? "<ESC>" : "")));
      return <Text>{`got=${last}`}</Text>;
    }
    const handle = mount(<Echo />, {
      viewportWidth: 20,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
      stdin,
    });
    await flush();
    expect(w.output()).toContain("got=");
    w.flush();
    stdin.push("k");
    await flush();
    expect(w.output()).toContain("k");
    handle.destroy();
  });

  it("hook stops receiving after component unmounts", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    let observed = 0;
    function Watcher() {
      useKeystroke(() => {
        observed++;
      });
      return <Text>w</Text>;
    }
    const handle = mount(<Watcher />, {
      viewportWidth: 5,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
      stdin,
    });
    await flush();
    stdin.push("a");
    await flush();
    expect(observed).toBe(1);
    handle.destroy();
    stdin.push("b");
    expect(observed).toBe(1);
  });
});
