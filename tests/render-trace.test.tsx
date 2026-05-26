import React from "react";
import { describe, expect, it } from "vitest";
import { renderTraceEnabled, useRenderTrace } from "../src/cli/ui/render-trace.js";
import { render } from "./helpers/ink-test.js";

describe("render-trace", () => {
  it("is disabled when REASONIX_TRACE_RENDERS is unset (default for tests)", () => {
    expect(renderTraceEnabled).toBe(false);
  });

  it("does not throw when called from a component body", () => {
    function Probe(): React.ReactElement {
      useRenderTrace("Probe");
      return React.createElement("text", null, "hi");
    }
    const { unmount } = render(React.createElement(Probe));
    unmount();
  });

  it("safe to call from a component that re-renders", () => {
    function Probe({ n }: { n: number }): React.ReactElement {
      useRenderTrace("Probe");
      return React.createElement("text", null, `${n}`);
    }
    const { rerender, unmount } = render(React.createElement(Probe, { n: 1 }));
    rerender(React.createElement(Probe, { n: 2 }));
    rerender(React.createElement(Probe, { n: 3 }));
    unmount();
  });
});
