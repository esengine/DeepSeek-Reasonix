import { describe, expect, it } from "vitest";
import { classifyDesktopQQIngress } from "../src/desktop/qq-ingress.js";

describe("classifyDesktopQQIngress", () => {
  it("treats pending prompt replies as follow-up replies even while busy", () => {
    expect(
      classifyDesktopQQIngress({
        hasPendingInteraction: true,
        isBusy: true,
      }),
    ).toBe("pause_reply");
  });

  it("rejects new turns while the active tab is busy", () => {
    expect(
      classifyDesktopQQIngress({
        hasPendingInteraction: false,
        isBusy: true,
      }),
    ).toBe("busy");
  });

  it("accepts a new turn only when there is no pending interaction and the tab is idle", () => {
    expect(
      classifyDesktopQQIngress({
        hasPendingInteraction: false,
        isBusy: false,
      }),
    ).toBe("new_turn");
  });
});
