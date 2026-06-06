import {
  copyFileSync,
  existsSync,
  mkdtempSync,
  readFileSync,
  renameSync,
  rmSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { type AtomicWriteFs, atomicWriteSync } from "../src/core/atomic-write.js";

const realFs: AtomicWriteFs = {
  writeFileSync,
  chmodSync: () => {},
  renameSync,
  copyFileSync,
  unlinkSync,
};

describe("atomicWriteSync", () => {
  let dir: string;
  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "atomic-write-"));
  });
  afterEach(() => {
    rmSync(dir, { recursive: true, force: true });
  });

  it("writes via rename on the happy path and leaves no tmp behind", () => {
    const target = join(dir, "config.json");
    const tmp = `${target}.tmp`;
    atomicWriteSync(target, '{"a":1}', tmp);
    expect(readFileSync(target, "utf8")).toBe('{"a":1}');
    expect(existsSync(tmp)).toBe(false);
  });

  it("falls back to copy on EXDEV (OneDrive / NTFS reparse points, #1738)", () => {
    const target = join(dir, "config.json");
    const tmp = `${target}.tmp`;
    let renameAttempted = false;
    const fs: AtomicWriteFs = {
      ...realFs,
      renameSync: () => {
        renameAttempted = true;
        const err = new Error("EXDEV: cross-device link not permitted") as NodeJS.ErrnoException;
        err.code = "EXDEV";
        err.errno = -4037;
        throw err;
      },
    };
    atomicWriteSync(target, '{"a":1}', tmp, 0o600, fs);
    expect(renameAttempted).toBe(true);
    expect(readFileSync(target, "utf8")).toBe('{"a":1}');
    expect(existsSync(tmp)).toBe(false);
  });

  it("rethrows non-EXDEV rename errors and cleans up tmp", () => {
    const target = join(dir, "config.json");
    const tmp = `${target}.tmp`;
    const fs: AtomicWriteFs = {
      ...realFs,
      renameSync: () => {
        const err = new Error("EACCES: permission denied") as NodeJS.ErrnoException;
        err.code = "EACCES";
        throw err;
      },
    };
    expect(() => atomicWriteSync(target, '{"a":1}', tmp, 0o600, fs)).toThrow(/EACCES/);
    expect(existsSync(tmp)).toBe(false);
  });
});
