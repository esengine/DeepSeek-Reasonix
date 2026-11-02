// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { clearToasts, pushNote, pushOffer, pushToast } from "../state/toasts";
import { ToastStack } from "./ToastStack";

globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;

afterEach(() => { cleanup(); clearToasts(); });

describe("a note in the window's corner", () => {
  it("is not drawn as a failure: no alert role, no report button, and it can be dismissed", () => {
    const onFeedback = vi.fn();
    render(<ToastStack onFeedback={onFeedback} />);
    act(() => pushNote("已把「X」从标签栏收起，对话仍在列表里。"));
    const note = screen.getByText("已把「X」从标签栏收起，对话仍在列表里。").closest(".toast")!;
    expect(note.getAttribute("role")).toBe("status");
    expect(note.classList.contains("note")).toBe(true);
    expect(note.querySelector('[data-action="toast.report"]')).toBeNull();
    fireEvent.click(note.querySelector('[data-action="toast.dismiss"]')!);
    expect(screen.queryByText("已把「X」从标签栏收起，对话仍在列表里。")).toBeNull();
  });

  it("does not take focus when it appears", () => {
    render(<><button data-testid="mine">mine</button><ToastStack onFeedback={() => {}} /></>);
    screen.getByTestId("mine").focus();
    act(() => pushNote("note"));
    expect(document.activeElement).toBe(screen.getByTestId("mine"));
  });

  it("leaves a failure beside it exactly as it was", () => {
    render(<ToastStack onFeedback={() => {}} />);
    act(() => { pushToast("it broke"); pushNote("fyi"); });
    const failure = screen.getByText("it broke").closest(".toast")!;
    expect(failure.getAttribute("role")).toBe("alert");
    expect(failure.querySelector('[data-action="toast.report"]')).not.toBeNull();
  });

  it("runs what a note offers when its button is pressed, and then goes away", () => {
    const run = vi.fn();
    render(<ToastStack onFeedback={() => {}} />);
    act(() => pushOffer("太大了", { label: "取最近的内容", run }));
    const note = screen.getByText("太大了").closest(".toast")!;
    expect(note.getAttribute("role")).toBe("status");
    fireEvent.click(note.querySelector('[data-action="toast.act"]')!);
    expect(run).toHaveBeenCalledTimes(1);
    expect(screen.queryByText("太大了")).toBeNull();
  });

  it("offers no button when the note has nothing to offer", () => {
    render(<ToastStack onFeedback={() => {}} />);
    act(() => pushNote("fyi"));
    expect(document.querySelector('[data-action="toast.act"]')).toBeNull();
  });
});
