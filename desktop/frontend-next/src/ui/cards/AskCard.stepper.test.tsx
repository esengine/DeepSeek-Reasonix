// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Item } from "../../state/session";
import { AskCard } from "./AskCard";

afterEach(cleanup);

const ask = (n: number) =>
  ({
    t: "ask",
    id: "row",
    ask: {
      id: "ask",
      questions: Array.from({ length: n }, (_, i) => ({
        id: `q${i + 1}`,
        header: `Q${i + 1}`,
        prompt: `question ${i + 1}`,
        multi: true,
        options: [{ label: `a${i + 1}` }, { label: `b${i + 1}` }],
      })),
    },
  }) as Extract<Item, { t: "ask" }>;

const tab = (name: string) => screen.getByRole("tab", { name: new RegExp(name) });
const on = (name: string) => expect(tab(name).getAttribute("aria-selected")).toBe("true");
const click = (name: string | RegExp) => userEvent.click(screen.getByRole("button", { name }));

describe("ask stepper after going back", () => {
  it("Next from an answered question lands on the following one, not the last", async () => {
    render(<AskCard item={ask(3)} onAnswer={vi.fn()} />);
    await click(/a1/);
    await click("下一题（1/3）");
    on("Q2");
    await click(/a2/);
    await click("上一题");
    on("Q1");
    expect(screen.getByRole("button", { name: "下一题（1/3）" })).toBeTruthy();
    await click("下一题（1/3）");
    on("Q2");
    expect(screen.getByRole("button", { name: "下一题（2/3）" })).toBeTruthy();
  });

  it("with two questions Next stays available on the first after going back", async () => {
    render(<AskCard item={ask(2)} onAnswer={vi.fn()} />);
    await click(/a1/);
    await click("下一题（1/2）");
    await click(/a2/);
    await click("上一题");
    on("Q1");
    expect(screen.queryByRole("button", { name: "确认" })).toBeNull();
    await click("下一题（1/2）");
    on("Q2");
    expect(screen.getByRole("button", { name: "确认" })).toBeTruthy();
  });

  it("keeps answers across back and forth and submits them", async () => {
    const answer = vi.fn(async () => {});
    render(<AskCard item={ask(3)} onAnswer={answer} />);
    await click(/a1/);
    await click("下一题（1/3）");
    await click(/b2/);
    await click("下一题（2/3）");
    await click(/a3/);
    await click("上一题");
    await click("上一题");
    on("Q1");
    await click("下一题（1/3）");
    await click("下一题（2/3）");
    await click("确认");
    await waitFor(() =>
      expect(answer).toHaveBeenCalledWith("row", "ask", [
        { questionId: "q1", selected: ["a1"] },
        { questionId: "q2", selected: ["b2"] },
        { questionId: "q3", selected: ["a3"] },
      ]),
    );
  });

  it("tab clicks and Enter in the free-text box follow the same sequential rule", async () => {
    render(<AskCard item={ask(3)} onAnswer={vi.fn()} />);
    await click(/a1/);
    await click("下一题（1/3）");
    await click(/a2/);
    await click("下一题（2/3）");
    await click(/a3/);
    await userEvent.click(tab("Q1"));
    on("Q1");
    const box = screen.getAllByRole("textbox")[0];
    await userEvent.click(screen.getAllByRole("button", { name: /其他/ })[0]);
    await userEvent.type(box, "x{Enter}");
    on("Q2");
  });
});
