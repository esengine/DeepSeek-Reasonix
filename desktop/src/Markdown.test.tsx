// @vitest-environment jsdom

import { render } from "@testing-library/react";
import { beforeAll, describe, expect, it, vi } from "vitest";

vi.mock("@tauri-apps/api/core", () => ({ invoke: vi.fn() }));
vi.mock("@tauri-apps/plugin-opener", () => ({ openPath: vi.fn(), openUrl: vi.fn() }));

import { Markdown } from "./Markdown";

beforeAll(() => {
  Object.defineProperty(navigator, "clipboard", {
    value: { writeText: vi.fn() },
    configurable: true,
  });
});

describe("Markdown", () => {
  it("wraps tables in a horizontal scroll container", () => {
    const { container } = render(
      <Markdown
        source={`| A | B | C | D |
| - | - | - | - |
| 1 | 2 | 3 | 4 |`}
      />,
    );

    const wrap = container.querySelector(".markdown-table-wrap");
    expect(wrap).toBeTruthy();
    expect(wrap?.querySelector("table")).toBeTruthy();
  });
});
