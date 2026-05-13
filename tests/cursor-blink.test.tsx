import { Text } from "ink";
import { render } from "ink-testing-library";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TickerProvider, useCursorBlink } from "../src/cli/ui/ticker.js";

function Probe(): React.ReactElement {
  return <Text>{useCursorBlink() ? "ON" : "OFF"}</Text>;
}

describe("useCursorBlink — issue #728", () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "performance"] });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("stays visible when the ticker disables after an odd frame (issue #728)", async () => {
    const { lastFrame, rerender, unmount } = render(
      <TickerProvider disabled={false}>
        <Probe />
      </TickerProvider>,
    );
    await vi.advanceTimersByTimeAsync(1100);
    rerender(
      <TickerProvider disabled={true}>
        <Probe />
      </TickerProvider>,
    );
    const out = lastFrame() ?? "";
    unmount();
    expect(out).toContain("ON");
  });
});
