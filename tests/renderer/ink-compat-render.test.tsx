// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { describe, expect, it } from "vitest";
import { Box, Text, render, useApp, useInput } from "../../src/renderer/ink-compat/index.js";

interface FakeWriteStream {
  columns: number;
  rows: number;
  isTTY: boolean;
  write: (chunk: string) => boolean;
  on: (event: "resize", cb: () => void) => void;
  off: (event: "resize", cb: () => void) => void;
  emit: (event: "resize") => void;
  data: string[];
}

function makeFakeStdout(width = 60, height = 12): FakeWriteStream {
  const listeners = new Set<() => void>();
  const data: string[] = [];
  return {
    columns: width,
    rows: height,
    isTTY: true,
    write(chunk) {
      data.push(chunk);
      return true;
    },
    on(_event, cb) {
      listeners.add(cb);
    },
    off(_event, cb) {
      listeners.delete(cb);
    },
    emit() {
      for (const cb of listeners) cb();
    },
    data,
  };
}

interface FakeStdin {
  isTTY: boolean;
  on: (event: "data", cb: (c: string | Buffer) => void) => void;
  off: (event: "data", cb: (c: string | Buffer) => void) => void;
  setRawMode: (raw: boolean) => void;
  resume: () => void;
  pause: () => void;
  push: (s: string) => void;
}

function makeFakeStdin(): FakeStdin {
  const listeners = new Set<(c: string | Buffer) => void>();
  return {
    isTTY: true,
    on(_event, cb) {
      listeners.add(cb);
    },
    off(_event, cb) {
      listeners.delete(cb);
    },
    setRawMode() {},
    resume() {},
    pause() {},
    push(s) {
      for (const cb of listeners) cb(s);
    },
  };
}

function flush(): Promise<void> {
  return new Promise((r) => setTimeout(r, 0));
}

describe("inkCompat.render — basic mount", () => {
  it("renders a Box + Text tree", async () => {
    const stdout = makeFakeStdout();
    const stdin = makeFakeStdin();
    const inst = render(
      <Box flexDirection="column">
        <Text>hello cell-diff</Text>
      </Box>,
      {
        stdout: stdout as unknown as NodeJS.WriteStream,
        stdin: stdin as unknown as NodeJS.ReadStream,
      },
    );
    await flush();
    const out = stdout.data.join("");
    expect(out).toContain("hello cell-diff");
    inst.unmount();
    await inst.waitUntilExit();
  });

  it("waitUntilExit resolves only after unmount", async () => {
    const stdout = makeFakeStdout();
    const stdin = makeFakeStdin();
    const inst = render(<Text>x</Text>, {
      stdout: stdout as unknown as NodeJS.WriteStream,
      stdin: stdin as unknown as NodeJS.ReadStream,
    });
    let resolved = false;
    const exit = inst.waitUntilExit().then(() => {
      resolved = true;
    });
    await flush();
    expect(resolved).toBe(false);
    inst.unmount();
    await exit;
    expect(resolved).toBe(true);
  });

  it("rerender swaps the displayed text", async () => {
    const stdout = makeFakeStdout();
    const stdin = makeFakeStdin();
    const inst = render(<Text>first</Text>, {
      stdout: stdout as unknown as NodeJS.WriteStream,
      stdin: stdin as unknown as NodeJS.ReadStream,
    });
    await flush();
    const before = stdout.data.length;
    inst.rerender(<Text>second</Text>);
    await flush();
    const out = stdout.data.slice(before).join("");
    expect(out).toContain("second");
    inst.unmount();
    await inst.waitUntilExit();
  });
});

describe("inkCompat.render — Ctrl+C", () => {
  it("Ctrl+C on stdin unmounts when exitOnCtrlC is on (default)", async () => {
    const stdout = makeFakeStdout();
    const stdin = makeFakeStdin();
    const inst = render(<Text>busy</Text>, {
      stdout: stdout as unknown as NodeJS.WriteStream,
      stdin: stdin as unknown as NodeJS.ReadStream,
    });
    await flush();
    let resolved = false;
    const exit = inst.waitUntilExit().then(() => {
      resolved = true;
    });
    stdin.push("\x03");
    await flush();
    await exit;
    expect(resolved).toBe(true);
  });

  it("exitOnCtrlC: false ignores Ctrl+C bytes", async () => {
    const stdout = makeFakeStdout();
    const stdin = makeFakeStdin();
    const inst = render(<Text>busy</Text>, {
      stdout: stdout as unknown as NodeJS.WriteStream,
      stdin: stdin as unknown as NodeJS.ReadStream,
      exitOnCtrlC: false,
    });
    await flush();
    let resolved = false;
    inst.waitUntilExit().then(() => {
      resolved = true;
    });
    stdin.push("\x03");
    await flush();
    await flush();
    expect(resolved).toBe(false);
    inst.unmount();
  });
});

describe("inkCompat.render — useApp().exit() integration", () => {
  it("a child calling useApp().exit() drives waitUntilExit to completion", async () => {
    const stdout = makeFakeStdout();
    const stdin = makeFakeStdin();
    function Bye(): React.ReactElement {
      const { exit } = useApp();
      React.useEffect(() => {
        const id = setTimeout(() => exit(), 10);
        return () => clearTimeout(id);
      }, [exit]);
      return <Text>about to exit</Text>;
    }
    const inst = render(<Bye />, {
      stdout: stdout as unknown as NodeJS.WriteStream,
      stdin: stdin as unknown as NodeJS.ReadStream,
    });
    await inst.waitUntilExit();
  });
});

describe("inkCompat.render — useInput is wired", () => {
  it("typed bytes reach a useInput consumer inside the rendered tree", async () => {
    const stdout = makeFakeStdout();
    const stdin = makeFakeStdin();
    let captured = "";
    function Listener(): React.ReactElement {
      useInput((input) => {
        captured += input;
      });
      return <Text>x</Text>;
    }
    const inst = render(<Listener />, {
      stdout: stdout as unknown as NodeJS.WriteStream,
      stdin: stdin as unknown as NodeJS.ReadStream,
    });
    await flush();
    stdin.push("hi");
    await flush();
    expect(captured).toContain("h");
    expect(captured).toContain("i");
    inst.unmount();
  });
});
