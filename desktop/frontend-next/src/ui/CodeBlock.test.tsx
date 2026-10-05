// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { CodeBlock, extensionFor } from "./CodeBlock";

afterEach(cleanup);

const lines = (n: number) => Array.from({ length: n }, (_, i) => `line ${i + 1}`).join("\n") + "\n";
const draw = (source: string, live = false) =>
  render(<CodeBlock lang="go" source={source} live={live}><code>{source}</code></CodeBlock>).container;

// A fenced block says what it is and how long before it is read, numbers its
// lines, and does not let one long file push the rest of a reply off screen.
describe("a code block", () => {
  it("heads itself with its language and length, and numbers every line", () => {
    const box = draw(lines(3));
    expect(box.querySelector(".code-lang")?.textContent).toBe("go");
    expect(box.querySelector(".code-lines")?.textContent).toContain("3");
    expect([...box.querySelectorAll(".code-gutter > span")].map((li) => li.textContent)).toEqual(["1", "2", "3"]);
  });

  it("folds a long settled block and opens it on request", async () => {
    const box = draw(lines(40));
    expect(box.querySelector(".code-wrap")?.hasAttribute("data-folded")).toBe(true);
    await userEvent.click(screen.getByRole("button", { name: /40/ }));
    expect(box.querySelector(".code-wrap")?.hasAttribute("data-folded")).toBe(false);
  });

  it("never folds a block still arriving, or a short one", () => {
    expect(draw(lines(40), true).querySelector("[data-folded], .code-more")).toBeNull();
    cleanup();
    expect(draw(lines(5)).querySelector("[data-folded], .code-more")).toBeNull();
  });

  it("saves under the language's own suffix, or as text", () => {
    expect(extensionFor("Go")).toBe("go");
    expect(extensionFor("typescript")).toBe("ts");
    expect(extensionFor("brainfuck")).toBe("txt");
  });
});
