// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { QuestionNav, type QuestionNavItem } from "./question-nav";

afterEach(cleanup);

const items: QuestionNavItem[] = [
  { messageIndex: 0, ordinal: 1, turn: 1, text: "How do I start the desktop app?" },
  { messageIndex: 4, ordinal: 2, turn: 2, text: "Add a right-side question navigation rail." },
];

describe("QuestionNav", () => {
  it("renders one jump target per user question", () => {
    render(<QuestionNav items={items} activeMessageIndex={4} onPick={() => {}} />);

    expect(screen.getByRole("navigation", { name: "Question navigation" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Jump to question 1" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Jump to question 2" })).toBeTruthy();
  });

  it("calls onPick with the message index", () => {
    const onPick = vi.fn();
    render(<QuestionNav items={items} activeMessageIndex={0} onPick={onPick} />);

    fireEvent.click(screen.getByRole("button", { name: "Jump to question 2" }));

    expect(onPick).toHaveBeenCalledWith(4);
  });
});
