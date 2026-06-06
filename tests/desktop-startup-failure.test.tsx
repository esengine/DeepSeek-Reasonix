// @vitest-environment jsdom

import { fireEvent, render, screen } from "@testing-library/react";
import React from "react";
import { describe, expect, it, vi } from "vitest";
import { StartupFailure, coerceStartupFailure } from "../desktop/src/ui/startup-failure";

describe("desktop startup failure screen (#1752)", () => {
  it("renders backend startup failures instead of leaving the window blank", () => {
    const retry = vi.fn();
    render(<StartupFailure details={["spawn: dist/cli/index.js not found"]} onRetry={retry} />);

    expect(screen.getByRole("alert").textContent).toContain("Reasonix could not start");
    expect(screen.getByText("spawn: dist/cli/index.js not found")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(retry).toHaveBeenCalledTimes(1);
  });

  it("keeps the original error and recent stderr lines for diagnosis", () => {
    expect(coerceStartupFailure(new Error("spawn failed"), ["stderr a", "stderr b"])).toEqual({
      details: ["spawn failed", "stderr a", "stderr b"],
    });
  });
});
