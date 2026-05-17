import { describe, expect, it } from "vitest";
import {
  HEAP_HEADROOM_MB,
  TARGET_HEAP_MB_CEILING,
  TARGET_HEAP_MB_FLOOR,
  decideHeapTargetMb,
} from "../src/cli/heap-limit.js";

describe("decideHeapTargetMb (issue #1011)", () => {
  const base = {
    nodeOptions: "",
    execArgv: [] as readonly string[],
    alreadyReexec: false,
  };

  it("targets 4 GiB on a typical workstation with the default 2 GiB cap", () => {
    expect(decideHeapTargetMb({ ...base, currentLimitMb: 2090, totalMemMb: 16384 })).toBe(
      TARGET_HEAP_MB_CEILING,
    );
  });

  it("scales target down to half of system memory on a memory-constrained VM", () => {
    // 6 GiB total → half is 3072 MiB, which is between floor and ceiling.
    expect(decideHeapTargetMb({ ...base, currentLimitMb: 2090, totalMemMb: 6144 })).toBe(3072);
  });

  it("clamps target to the 2 GiB floor on small-memory systems instead of going lower", () => {
    // 1 GiB total — half is 512 MiB; without the floor clamp we'd target
    // 512 MiB and shrink the heap. Pretend the current limit is below the
    // floor so we exercise the raise path on this small-memory case.
    expect(decideHeapTargetMb({ ...base, currentLimitMb: 1024, totalMemMb: 1024 })).toBe(
      TARGET_HEAP_MB_FLOOR,
    );
  });

  it("returns null when already above the target (within headroom)", () => {
    expect(
      decideHeapTargetMb({
        ...base,
        currentLimitMb: TARGET_HEAP_MB_CEILING - HEAP_HEADROOM_MB + 1,
        totalMemMb: 16384,
      }),
    ).toBeNull();
  });

  it("returns null when the user already set --max-old-space-size in NODE_OPTIONS", () => {
    expect(
      decideHeapTargetMb({
        ...base,
        currentLimitMb: 2090,
        totalMemMb: 16384,
        nodeOptions: "--max-old-space-size=8192",
      }),
    ).toBeNull();
    expect(
      decideHeapTargetMb({
        ...base,
        currentLimitMb: 2090,
        totalMemMb: 16384,
        nodeOptions: "--inspect --max_old_space_size=8192",
      }),
    ).toBeNull();
  });

  it("returns null when the user passed --max-old-space-size on the command line (execArgv)", () => {
    expect(
      decideHeapTargetMb({
        ...base,
        currentLimitMb: 2090,
        totalMemMb: 16384,
        execArgv: ["--max-old-space-size=6144"],
      }),
    ).toBeNull();
  });

  it("returns null after a successful re-exec (REASONIX_HEAP_REEXEC=1 set)", () => {
    expect(
      decideHeapTargetMb({
        ...base,
        currentLimitMb: 2090,
        totalMemMb: 16384,
        alreadyReexec: true,
      }),
    ).toBeNull();
  });

  it("does NOT re-exec when current limit is already at the target", () => {
    expect(
      decideHeapTargetMb({
        ...base,
        currentLimitMb: TARGET_HEAP_MB_CEILING,
        totalMemMb: 16384,
      }),
    ).toBeNull();
  });
});
