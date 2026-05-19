/** #1213 — discoverability net: /skill must remain in the empty-session hint row. */

import { render } from "ink-testing-library";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { describe, expect, it } from "vitest";
import { WelcomeBanner } from "../src/cli/ui/WelcomeBanner.js";

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
