// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, waitFor } from "@testing-library/react";
import type { Item } from "../../state/session";
import { AskCard } from "./AskCard";

afterEach(cleanup);

it("formats Markdown in the ask prompt", async () => {
  const item = {
    t: "ask",
    id: "row",
    ask: {
      id: "call",
      questions: [{
        id: "q1",
        header: "选择",
        prompt: "请选择 **重点** 方案\n\n- 方案 A\n- 方案 B",
        options: [{ label: "方案 A" }],
      }],
    },
  } as Extract<Item, { t: "ask" }>;

  const { container } = render(<AskCard item={item} onAnswer={vi.fn()} />);
  await waitFor(() => expect(container.querySelector(".ask-q strong")?.textContent).toBe("重点"));
  expect([...container.querySelectorAll(".ask-q li")].map((li) => li.textContent)).toEqual(["方案 A", "方案 B"]);
});
