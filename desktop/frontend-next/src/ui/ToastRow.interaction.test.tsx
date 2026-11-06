// @vitest-environment jsdom
import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import "./testkit";
import { clearToasts, pushNote, pushOffer, pushToast } from "../state/toasts";
import { ToastStack } from "./ToastStack";

afterEach(() => {
  cleanup();
  clearToasts();
});

describe("a corner notice carries one shape", () => {
  it("leads with an icon and draws its actions as the shared small ghost buttons", () => {
    render(<ToastStack onFeedback={vi.fn()} />);
    act(() => {
      pushToast("模型切换失败");
      pushNote("已另存为新会话");
      pushOffer("历史太大", { label: "取最近的内容", run: vi.fn() });
    });
    const rows = document.querySelectorAll(".toast");
    expect(rows).toHaveLength(3);
    for (const row of rows) {
      expect(row.querySelector(".toast-icon .studio-icon")).not.toBeNull();
      const buttons = row.querySelectorAll("button");
      expect(buttons.length).toBeGreaterThan(0);
      for (const b of buttons) expect(b.classList.contains("act")).toBe(true);
    }
  });

  it("tells a failure from a note by its icon, not only its colour", () => {
    render(<ToastStack onFeedback={vi.fn()} />);
    act(() => {
      pushToast("失败了");
      pushNote("只是说明");
    });
    const [failure, note] = [
      ...document.querySelectorAll(".toast .toast-icon"),
    ];
    expect(failure.getAttribute("data-kind")).toBe("error");
    expect(note.getAttribute("data-kind")).toBe("note");
  });

  it("keeps the dismiss button on every notice", () => {
    render(<ToastStack onFeedback={vi.fn()} />);
    act(() => {
      pushToast("一");
      pushNote("二");
    });
    expect(screen.getAllByRole("button", { name: "知道了" })).toHaveLength(2);
  });

  it("shows at most the three newest, oldest first", () => {
    render(<ToastStack onFeedback={vi.fn()} />);
    act(() => {
      for (const n of ["一", "二", "三", "四", "五"]) pushNote(n);
    });
    const shown = [...document.querySelectorAll(".toast .toast-body")].map(
      (el) => el.textContent,
    );
    expect(shown).toEqual(["三", "四", "五"]);
    act(() => {
      screen.getAllByRole("button", { name: "知道了" })[2].click();
    });
    expect(
      [...document.querySelectorAll(".toast .toast-body")].map(
        (el) => el.textContent,
      ),
    ).toEqual(["三", "四"]);
  });
});
