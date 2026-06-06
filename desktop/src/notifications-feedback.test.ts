import { describe, expect, it } from "vitest";
import { COMPLETION_NOTIFY_MIN_MS, shouldShowCompletionToast } from "./notifications";

describe("desktop completion feedback", () => {
  it("shows in-window feedback when a long task completes while focused", () => {
    expect(
      shouldShowCompletionToast({
        wasBusy: true,
        isBusy: false,
        busyDurationMs: COMPLETION_NOTIFY_MIN_MS,
        focused: true,
      }),
    ).toBe(true);
  });

  it("does not show in-window feedback for short tasks", () => {
    expect(
      shouldShowCompletionToast({
        wasBusy: true,
        isBusy: false,
        busyDurationMs: COMPLETION_NOTIFY_MIN_MS - 1,
        focused: true,
      }),
    ).toBe(false);
  });
});
