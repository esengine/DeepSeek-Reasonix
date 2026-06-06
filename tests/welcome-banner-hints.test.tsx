/** #1213 — discoverability net: /skill must remain in the empty-session hint row. */

import React from "react";
import { describe, expect, it } from "vitest";
import { WelcomeBanner } from "../src/cli/ui/WelcomeBanner.js";
import { render } from "./helpers/ink-test.js";

describe("WelcomeBanner hint row", () => {
  it("surfaces /skill alongside the other starter commands (#1213)", () => {
    const { lastFrame } = render(<WelcomeBanner />);
    const frame = lastFrame() ?? "";
    expect(frame).toContain("/help");
    expect(frame).toContain("/skill");
    expect(frame).toContain("/init");
    expect(frame).toContain("/memory");
    expect(frame).toContain("/cost");
  });
});
