import { describe, expect, it } from "vitest";
import css from "./queue.css?raw";

describe("the send-now button follows the board", () => {
  it("is a 24px pill with a hairline border on the raised surface", () => {
    const rule = css.match(/\.qacts button\.qnow \{([^}]*)\}/)?.[1] ?? "";
    expect(rule).toMatch(/height:\s*24px/);
    expect(rule).toMatch(/border:\s*1px solid var\(--border\)/);
    expect(rule).toMatch(/border-radius:\s*var\(--r-pill\)/);
    expect(rule).toMatch(/background:\s*var\(--raised\)/);
  });
});
