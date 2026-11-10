// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, waitFor } from "@testing-library/react";
import { Markdown } from "./Markdown";

afterEach(cleanup);

const rendered = async (text: string) => {
  const view = render(<Markdown text={text} />);
  await waitFor(() => expect(view.container.querySelector(".katex, .katex-error")).toBeTruthy());
  return view.container;
};

describe("a formula that only a display equation can carry", () => {
  it("renders a \\tag written on one $$ line", async () => {
    const c = await rendered("论文原文：\n\n$$\\theta^l\\sim p(\\theta^l),\\quad 1\\le l\\le L \\tag{3}$$\n");
    expect(c.querySelector(".katex-error")).toBeNull();
    expect(c.querySelector(".katex-display")).toBeTruthy();
  });

  it("renders a \\tag inside a \\[ … \\] block", async () => {
    const c = await rendered("式 (5)：\n\n\\[\\boxed{p(y\\mid x,D)=\\int p(y\\mid x,\\theta)\\,p(\\theta\\mid D)\\,d\\theta} \\tag{5}\\]\n");
    expect(c.querySelector(".katex-error")).toBeNull();
    expect(c.querySelector(".katex-display")).toBeTruthy();
  });

  it("leaves a $$ pair inside a sentence to read as inline math", async () => {
    const c = await rendered("因为 $$x^2$$ 总是非负。");
    expect(c.querySelector(".katex-error")).toBeNull();
    expect(c.querySelector(".katex-display")).toBeNull();
  });

  it("does not touch a $$ line inside a code fence", async () => {
    const c = render(<Markdown text={"```\n$$a \\tag{1}$$\n```"} />).container;
    expect(c.querySelector("code")?.textContent).toContain("$$a \\tag{1}$$");
  });
});
