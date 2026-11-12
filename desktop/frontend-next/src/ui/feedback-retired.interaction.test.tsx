// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { FEEDBACK_RETIRED } from "../feedbackchannel";
import { Nav } from "./Nav";
import { ErrorBar } from "./ErrorBar";

afterEach(cleanup);

describe("the retired feedback channel", () => {
  it("is retired in the shipped build", () => {
    expect(FEEDBACK_RETIRED).toBe(true);
  });

  it("leaves no feedback entry on the nav rail", () => {
    render(<Nav at="" onGo={vi.fn()} onFiles={vi.fn()} onHome={vi.fn()} onRail={vi.fn()} onFeedback={vi.fn()} onNew={vi.fn()} feedbackUnread={3} shown />);
    expect(document.querySelector('[data-action="feedback.open"]')).toBeNull();
  });

  it("offers no way to report an error from its bar", () => {
    render(<ErrorBar text="boom" onDismiss={vi.fn()} />);
    expect(screen.queryByRole("button", { name: /报告|report/i })).toBeNull();
  });
});
