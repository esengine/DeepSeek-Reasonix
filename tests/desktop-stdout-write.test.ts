import { describe, expect, it } from "vitest";
import { writeAllSync } from "../src/cli/commands/desktop.js";

describe("desktop stdout writes", () => {
  it("continues after partial writes and EAGAIN", () => {
    const input = Buffer.from('{"type":"$session_loaded","messages":["large"]}\n', "utf8");
    const chunks: Buffer[] = [];
    let calls = 0;
    let waits = 0;

    writeAllSync(1, input, {
      write: (_fd, buffer, offset, length) => {
        calls++;
        if (calls === 1) {
          const written = Math.min(4, length);
          chunks.push(Buffer.from(buffer.subarray(offset, offset + written)));
          return written;
        }
        if (calls === 2) {
          const err = new Error("try again") as NodeJS.ErrnoException;
          err.code = "EAGAIN";
          throw err;
        }
        chunks.push(Buffer.from(buffer.subarray(offset, offset + length)));
        return length;
      },
      wait: () => {
        waits++;
      },
    });

    expect(Buffer.concat(chunks).toString("utf8")).toBe(input.toString("utf8"));
    expect(calls).toBe(3);
    expect(waits).toBe(1);
  });
});
