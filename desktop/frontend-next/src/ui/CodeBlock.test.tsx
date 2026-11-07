// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { CodeBlock, extensionFor } from "./CodeBlock";
import css from "../styles/app.css?raw";

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
    expect([...box.querySelectorAll(".code-no")].map((li) => li.textContent)).toEqual(["1", "2", "3"]);
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

// A long line wraps inside the block instead of scrolling it sideways, so its
// number has to stay with the first row of the line it names.
describe("a code block's rows", () => {
  const rows = (box: HTMLElement) => [...box.querySelectorAll(".code-ln")];

  it("is one row per source line, blank lines included, with no row for the closing newline", () => {
    const box = draw("a\n\nb\n");
    expect(rows(box).map((r) => r.querySelector(".code-no")?.textContent)).toEqual(["1", "2", "3"]);
    expect(rows(box).map((r) => r.querySelector(".code-tx")?.textContent?.replace(/\n$/, ""))).toEqual(["a", "", "b"]);
  });

  it("splits a highlighted element that spans a newline into one piece per row", () => {
    const source = "/* one\n two */ x\n";
    const box = render(
      <CodeBlock lang="go" source={source}>
        <code className="hljs"><span className="hljs-comment">{"/* one\n two */"}</span>{" x\n"}</code>
      </CodeBlock>,
    ).container;
    const [first, second] = rows(box);
    expect(first.querySelector(".hljs-comment")?.textContent).toBe("/* one");
    expect(second.querySelector(".hljs-comment")?.textContent).toBe(" two */");
    expect(second.querySelector(".code-tx")?.textContent).toContain(" x");
  });

  it("keeps the numbers out of what a reader selects and out of the accessibility tree", () => {
    const box = draw(lines(2));
    for (const no of box.querySelectorAll(".code-no")) expect(no.getAttribute("aria-hidden")).toBe("true");
  });

  it("wraps long lines in the stylesheet rather than scrolling the block", () => {
    const rule = css.match(/\.code-tx\s*\{([^}]*)\}/)?.[1] ?? "";
    expect(rule).toMatch(/white-space:\s*pre-wrap/);
    expect(rule).toMatch(/overflow-wrap:\s*anywhere/);
  });
});
