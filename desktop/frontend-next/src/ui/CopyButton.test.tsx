// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import "./testkit";
import { t } from "../i18n";
import { CodeBlock } from "./CodeBlock";
import { CopyButton } from "./CopyButton";

afterEach(cleanup);

// A copy button names what it copies wherever it says anything: on hover, to a
// screen reader, and in its visible text when it has some.
describe("a copy button", () => {
  it("on a code block, offers to copy the code rather than the response", () => {
    render(<CodeBlock lang="go" source={"x := 1\n"}><code>x := 1</code></CodeBlock>);
    const button = screen.getByRole("button", { name: t("复制这段代码") });
    expect(button.getAttribute("title")).toBe(t("复制这段代码"));
  });

  it("with a label and visible text, says that label instead of the response's", () => {
    render(<CopyButton text="/home/me/.reasonix/config.toml" label={t("复制路径")} />);
    const button = screen.getByRole("button", { name: t("复制路径") });
    expect(button.textContent).toBe(t("复制路径"));
    expect(button.getAttribute("title")).toBe(t("复制路径"));
  });

  it("without a label, still offers to copy the response", () => {
    render(<CopyButton text="an answer" iconOnly />);
    expect(screen.getByRole("button").getAttribute("title")).toBe(t("复制回复"));
  });
});
