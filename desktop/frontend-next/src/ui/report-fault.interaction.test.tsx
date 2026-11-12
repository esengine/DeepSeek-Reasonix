// @vitest-environment jsdom
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

// These tests describe the feedback channel while it exists; the shipped build has it retired.
vi.mock("../feedbackchannel", () => ({ FEEDBACK_RETIRED: false }));
import "./testkit";
import { ErrorBar } from "./ErrorBar";
import { ToastStack } from "./ToastStack";
import { noteFault } from "./fault";
import { dropDraft, heldDraft } from "./feedbackdraft";
import { clearToasts, pushToast } from "../state/toasts";

afterEach(() => {
  cleanup();
  clearToasts();
  dropDraft();
  vi.restoreAllMocks();
});

const SHOWN = "Cannot read properties of undefined (reading 'filter')";

function pageFault() {
  vi.spyOn(console, "error").mockImplementation(() => {});
  const e = new TypeError(SHOWN);
  e.stack = `TypeError: ${SHOWN}\n    at waiting (https://app.example.test/assets/index-1.js:12:34)`;
  noteFault(e, SHOWN);
}

describe("reporting a notice that is a defect of the page", () => {
  it("writes the error's name and where it came from under the notice's words, from the corner", async () => {
    pageFault();
    render(<ToastStack onFeedback={vi.fn()} />);
    act(() => pushToast(SHOWN));
    await userEvent.click(screen.getByRole("button", { name: "报告此问题" }));
    expect(heldDraft().body).toBe(`${SHOWN}\n\nTypeError: ${SHOWN}\n  at waiting (index-1.js:12:34)`);
  });

  it("does the same from the error bar, which has the entry too", async () => {
    pageFault();
    const onReport = vi.fn();
    render(<ErrorBar text={SHOWN} onDismiss={vi.fn()} onReport={onReport} />);
    await userEvent.click(screen.getByRole("button", { name: "报告此问题" }));
    expect(onReport).toHaveBeenCalledOnce();
    expect(heldDraft().body).toContain("at waiting (index-1.js:12:34)");
  });

  it("adds only the words for a refusal, and appends to a draft already there", async () => {
    render(<ErrorBar text="模型切换失败" onDismiss={vi.fn()} onReport={vi.fn()} />);
    await userEvent.click(screen.getByRole("button", { name: "报告此问题" }));
    expect(heldDraft().body).toBe("模型切换失败");
    cleanup();
    render(<ErrorBar text="第二个问题" onDismiss={vi.fn()} onReport={vi.fn()} />);
    await userEvent.click(screen.getByRole("button", { name: "报告此问题" }));
    expect(heldDraft().body).toBe("模型切换失败\n\n第二个问题");
  });

  it("offers no report entry when nobody can receive one", () => {
    render(<ErrorBar text="x" onDismiss={vi.fn()} />);
    expect(screen.queryByRole("button", { name: "报告此问题" })).toBeNull();
  });
});
