import { render } from "ink-testing-library";
// biome-ignore lint/style/useImportType: needed in value scope for JSX
import React from "react";
import { describe, expect, it } from "vitest";
import { UserCard } from "../src/cli/ui/cards/UserCard.js";
import type { UserCard as UserCardData } from "../src/cli/ui/state/cards.js";

function userCard(text: string): UserCardData {
  return { kind: "user", id: "u1", ts: Date.now(), text };
}

describe("UserCard renders verbatim (issue #655)", () => {
  it("preserves literal asterisks instead of treating them as bold/italic", () => {
    const { lastFrame } = render(<UserCard card={userCard("hit **error** in src/foo.ts")} />);
    expect(lastFrame()).toContain("**error**");
    expect(lastFrame()).toContain("src/foo.ts");
  });

  it("preserves a leading '#' instead of rendering it as a heading", () => {
    const { lastFrame } = render(<UserCard card={userCard("# why does this fail")} />);
    expect(lastFrame()).toContain("# why does this fail");
  });

  it("preserves bracket-link syntax verbatim (no link rewrite, no hidden url)", () => {
    const { lastFrame } = render(
      <UserCard card={userCard("see [docs](https://example.com/doc)")} />,
    );
    expect(lastFrame()).toContain("[docs](https://example.com/doc)");
  });

  it("preserves inline backticks", () => {
    const { lastFrame } = render(<UserCard card={userCard("call `web_search()`")} />);
    expect(lastFrame()).toContain("`web_search()`");
  });

  it("preserves four-space-indented blocks (markdown's code-block trigger)", () => {
    const { lastFrame } = render(
      <UserCard card={userCard("error trace:\n    at foo (bar.js:10:5)")} />,
    );
    const out = lastFrame();
    expect(out).toContain("    at foo (bar.js:10:5)");
  });
});
