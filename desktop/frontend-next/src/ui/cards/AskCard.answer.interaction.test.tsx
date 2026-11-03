// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Item } from "../../state/session";
import { AskCard } from "./AskCard";

afterEach(cleanup);

const card = (multi: boolean, answered?: string[][]) =>
  ({
    t: "ask",
    id: "row",
    ask: {
      id: "ask",
      questions: [{ id: "q1", header: "Q1", prompt: "which one", multi, options: [{ label: "a" }, { label: "b" }] }],
    },
    answered,
  }) as Extract<Item, { t: "ask" }>;

const box = () => screen.getByRole("textbox", { name: "其他 —— 自行填写" }) as HTMLInputElement;
const other = () => screen.getByRole("button", { name: /其他 —— 自行填写/ });

describe("ask card free-text answer", () => {
  it("typing without choosing Other first is an Other answer", async () => {
    const answer = vi.fn(async () => {});
    render(<AskCard item={card(false)} onAnswer={answer} />);
    await userEvent.type(box(), "my own plan");
    expect(other().getAttribute("aria-pressed")).toBe("true");
    await userEvent.click(screen.getByRole("button", { name: "确认" }));
    expect(answer).toHaveBeenCalledWith("row", "ask", [{ questionId: "q1", selected: ["my own plan"] }]);
  });

  it("clicking into the empty box chooses Other", async () => {
    render(<AskCard item={card(false)} onAnswer={vi.fn(async () => {})} />);
    await userEvent.click(box());
    expect(other().getAttribute("aria-pressed")).toBe("true");
    expect(document.activeElement).toBe(box());
  });

  it("clicking into the box beside multi-choice picks keeps them and adds Other", async () => {
    render(<AskCard item={card(true)} onAnswer={vi.fn(async () => {})} />);
    await userEvent.click(screen.getByRole("button", { name: "a" }));
    await userEvent.click(box());
    expect(other().getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByRole("button", { name: "a" }).getAttribute("aria-pressed")).toBe("true");
  });

  it("clicking into a single pick's note box keeps that pick", async () => {
    render(<AskCard item={card(false)} onAnswer={vi.fn(async () => {})} />);
    await userEvent.click(screen.getByRole("button", { name: "a" }));
    await userEvent.click(screen.getByRole("textbox", { name: "补充说明（可选）" }));
    expect(screen.getByRole("button", { name: "a" }).getAttribute("aria-pressed")).toBe("true");
    expect(other().getAttribute("aria-pressed")).toBe("false");
  });

  it("typing alongside multi-choice picks rides with them", async () => {
    const answer = vi.fn(async () => {});
    render(<AskCard item={card(true)} onAnswer={answer} />);
    await userEvent.click(screen.getByRole("button", { name: "a" }));
    await userEvent.type(box(), "and more");
    await userEvent.click(screen.getByRole("button", { name: "确认" }));
    expect(answer).toHaveBeenCalledWith("row", "ask", [{ questionId: "q1", selected: ["a", "and more"] }]);
  });

  it("after a single pick the same box is that pick's note", async () => {
    const answer = vi.fn(async () => {});
    render(<AskCard item={card(false)} onAnswer={answer} />);
    await userEvent.click(screen.getByRole("button", { name: "a" }));
    await userEvent.type(screen.getByRole("textbox", { name: "补充说明（可选）" }), "n");
    await userEvent.click(screen.getByRole("button", { name: "确认" }));
    expect(answer).toHaveBeenCalledWith("row", "ask", [{ questionId: "q1", selected: ["a", "n"] }]);
  });

  it("choosing Other still opens the box and Enter submits it", async () => {
    const answer = vi.fn(async () => {});
    render(<AskCard item={card(false)} onAnswer={answer} />);
    await userEvent.click(other());
    expect(document.activeElement).toBe(box());
    await userEvent.type(box(), "x{Enter}");
    expect(answer).toHaveBeenCalledWith("row", "ask", [{ questionId: "q1", selected: ["x"] }]);
  });

  it("skipping still sends an empty answer batch", async () => {
    const answer = vi.fn(async () => {});
    render(<AskCard item={card(false)} onAnswer={answer} />);
    await userEvent.click(screen.getByRole("button", { name: "先不选择，直接回复" }));
    expect(answer).toHaveBeenCalledWith("row", "ask", [{ questionId: "q1", selected: [] }]);
  });

  it("an answered card keeps the box read-only", () => {
    render(<AskCard item={card(false, [["a"]])} onAnswer={vi.fn()} />);
    expect(box().readOnly).toBe(true);
  });
});
