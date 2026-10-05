// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import type { Item } from "../../state/session";
import { AskCard } from "./AskCard";

afterEach(cleanup);

describe("an ask rebuilt from the record", () => {
  it("shows the question, every option and what the kernel recorded, and offers nothing to answer", () => {
    const item = {
      t: "ask",
      id: "row",
      ask: {
        id: "call_00_x",
        questions: [
          { id: "q1", header: "Library", prompt: "Which parser should we use?", options: [{ label: "goldmark", description: "CommonMark" }, { label: "blackfriday" }] },
        ],
      },
      answered: [],
      recorded: "The user answered:\n- Library: goldmark",
    } as Extract<Item, { t: "ask" }>;
    const { container } = render(<AskCard item={item} onAnswer={vi.fn()} />);

    expect(screen.getByText("Which parser should we use?")).toBeTruthy();
    expect(screen.getByText("goldmark")).toBeTruthy();
    expect(screen.getByText("CommonMark")).toBeTruthy();
    expect(screen.getByText("blackfriday")).toBeTruthy();
    expect(container.querySelector(".ask-done")?.textContent).toBe("The user answered:\n- Library: goldmark");
    expect(container.querySelector(".ask-foot")).toBeNull();
    expect(screen.queryByText("未答")).toBeNull();
  });
});
