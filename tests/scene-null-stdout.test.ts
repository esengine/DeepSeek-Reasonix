import { PassThrough } from "node:stream";
import { describe, expect, it } from "vitest";
import { makeNullStdout } from "../src/cli/ui/scene/null-stdout.js";

function fakeTty(columns: number, rows: number): NodeJS.WriteStream {
  const t = new PassThrough();
  Object.assign(t, { columns, rows, isTTY: true });
  return t as unknown as NodeJS.WriteStream;
}

describe("makeNullStdout", () => {
  it("discards writes without throwing", () => {
    const stdout = makeNullStdout(fakeTty(80, 24));
    expect(() => stdout.write("anything\n")).not.toThrow();
    expect(() => stdout.write(Buffer.from([0x1b, 0x5b, 0x32, 0x4a]))).not.toThrow();
  });

  it("reports the real tty's columns and rows", () => {
    const real = fakeTty(120, 48);
    const stdout = makeNullStdout(real);
    expect(stdout.columns).toBe(120);
    expect(stdout.rows).toBe(48);
  });

  it("reports isTTY=true so Ink's tty detection passes", () => {
    const stdout = makeNullStdout(fakeTty(80, 24));
    expect(stdout.isTTY).toBe(true);
  });

  it("re-emits resize when the real tty resizes", () => {
    const real = fakeTty(80, 24);
    const stdout = makeNullStdout(real);
    let observed: { cols: number; rows: number } | null = null;
    stdout.on("resize", () => {
      observed = { cols: stdout.columns, rows: stdout.rows };
    });
    Object.assign(real, { columns: 100, rows: 30 });
    real.emit("resize");
    expect(observed).toEqual({ cols: 100, rows: 30 });
  });
});
