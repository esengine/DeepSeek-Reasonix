import { describe, expect, it } from "vitest";
import css from "./queue.css?raw";

describe("the queue strip follows the board", () => {
  it("draws each line as a hairline row with the board's radius and padding inside the card", () => {
    const row = css.match(/\.queue \.qi \{([^}]*)\}/)?.[1] ?? "";
    expect(row).toMatch(/border:\s*1px solid var\(--hair\)/);
    expect(row).toMatch(/border-radius:\s*var\(--r-md\)/);
    expect(row).toMatch(/padding:\s*6px 8px 6px 10px/);
    expect(css.match(/\.queue \{([^}]*)\}/)?.[1]).toMatch(/margin:\s*8px 12px 0/);
  });

  it("draws the actions as 20px pills on the raised surface", () => {
    const rule = css.match(/\.queue \.qb \{([^}]*)\}/)?.[1] ?? "";
    expect(rule).toMatch(/height:\s*20px/);
    expect(rule).toMatch(/border:\s*1px solid var\(--border\)/);
    expect(rule).toMatch(/border-radius:\s*var\(--r-pill\)/);
    expect(rule).toMatch(/background:\s*var\(--raised\)/);
  });

  it("draws a waiting steer as a dashed bubble", () => {
    const rule = css.match(/\.steerwait \.sw-txt \{([^}]*)\}/)?.[1] ?? "";
    expect(rule).toMatch(/border:\s*1px dashed var\(--border\)/);
    expect(rule).toMatch(/opacity:\s*\.8/);
  });
});
