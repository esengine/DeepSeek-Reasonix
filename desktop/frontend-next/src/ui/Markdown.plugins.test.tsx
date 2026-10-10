// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import { cleanup, render, waitFor } from "@testing-library/react";
import { Markdown } from "./Markdown";

afterEach(cleanup);

// The deferred plugins are functions, and useState takes a function argument as
// a lazy initializer. Storing what the attacher returns hands unified a
// transformer to attach, which then runs with no tree: every card mounted after
// the first listing threw and fell back to its own source text.
it("renders markdown on a card mounted after the highlighter arrived", async () => {
  const listing = render(<Markdown text={"```go\nfunc main() {}\n```"} />);
  await waitFor(() => expect(listing.container.querySelector("code.hljs")).toBeTruthy());
  cleanup();

  const { container } = render(<Markdown text={"**粗体**与 `行内`"} />);
  expect(container.querySelector("strong")).toBeTruthy();
  expect(container.querySelector("code")).toBeTruthy();
});

// A lone tilde is how Chinese (and plenty of English) writes a range, so two
// ranges in one line must not strike out everything between them.
it("reads a single tilde as a range, and only a doubled one as strikethrough", () => {
  const { container } = render(<Markdown text={"转速 500~1000 或 2000~3000 转，~~旧值~~"} />);
  const struck = container.querySelectorAll("del");
  expect(struck).toHaveLength(1);
  expect(struck[0].textContent).toBe("旧值");
  expect(container.textContent).toContain("500~1000 或 2000~3000");
});

// Models often put an address in backticks. A code span holding nothing but a
// web address is a link; any other code span, and every fenced block, is not.
it("links a code span that holds exactly one web address", () => {
  const { container } = render(<Markdown text={"打开 `https://example.com/a?b=1` 查看"} />);
  const link = container.querySelector("a[href='https://example.com/a?b=1']");
  expect(link).toBeTruthy();
  expect(link?.querySelector("code")?.textContent).toBe("https://example.com/a?b=1");
});

it("leaves code spans that are not one web address as code", () => {
  const { container } = render(
    <Markdown text={"`curl https://example.com` 与 `file:///etc/hosts` 与 `http://`\n\n```\nhttps://example.com\n```"} />,
  );
  expect(container.querySelector("a")).toBeNull();
  expect(container.querySelectorAll("code").length).toBe(4);
});

// A Persian or Arabic reply reads right to left: every text block lets the
// browser take its direction from its own first strong character, while code
// keeps the document's left-to-right flow.
it("lets each text block take its own direction and leaves code alone", async () => {
  const text = [
    "این یک پاسخ است که `npm install` را اجرا می‌کند.",
    "",
    "English paragraph.",
    "",
    "## عنوان",
    "",
    "- مورد اول",
    "- مورد دوم",
    "",
    "> نقل قول",
    "",
    "| ستون | value |",
    "| --- | --- |",
    "| یک | one |",
    "",
    "```sh",
    "npm install",
    "```",
  ].join("\n");
  const { container } = render(<Markdown text={text} />);
  for (const sel of ["p", "h2", "ul", "blockquote", "th", "td"]) {
    const els = container.querySelectorAll(sel);
    expect(els.length, sel).toBeGreaterThan(0);
    els.forEach((el) => expect(el.getAttribute("dir"), sel).toBe("auto"));
  }
  expect(container.querySelector("ul > li")?.getAttribute("dir")).toBeNull();
  await waitFor(() => expect(container.querySelector("pre")).toBeTruthy());
  expect(container.querySelector("pre")?.closest("[dir]")).toBeNull();
});
