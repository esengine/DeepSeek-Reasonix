import { describe, expect, it } from "vitest";
import { jobElapsed } from "./jobElapsed";

describe("how long a background job has run", () => {
  it.each([
    [0, "0 秒"], [42_000, "42 秒"], [60_000, "1 分钟"], [741_000, "12 分 21 秒"], [242_000, "4 分 02 秒"], [3_600_000, "1 小时"], [3_723_000, "1 小时 2 分"], [-5, "0 秒"],
  ])("reads %i ms as %s", (ms, text) => expect(jobElapsed(ms)).toBe(text));
});
