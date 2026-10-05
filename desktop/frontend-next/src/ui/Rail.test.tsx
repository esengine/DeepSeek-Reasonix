// @vitest-environment jsdom
import { cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import "./testkit";
import { Rail, railLocalY, type RailMark } from "./Rail";

afterEach(cleanup);

const mark = (id: string, at: number, text: string): RailMark => ({
  id, at, text, block: 0, within: 0, of: 1, files: 0,
});

describe("conversation overview rail", () => {
  it("maps the pointer into layout coordinates under compact zoom", () => {
    // A 600px layout rail is only 540px tall on screen at 90% zoom. Its visual
    // centre must still address the 300px mark, not the unscaled 270px one.
    expect(railLocalY(100 + 270, 100, 540, 600)).toBe(300);
    expect(railLocalY(100 + 300, 100, 600, 600)).toBe(300);
  });

  it("keeps sparse messages in a compact ordered fisheye", async () => {
    const root = document.createElement("div");
    const flow = document.createElement("div");
    Object.defineProperties(root, {
      clientHeight: { configurable: true, value: 600 },
      scrollHeight: { configurable: true, value: 2400 },
      scrollTop: { configurable: true, writable: true, value: 0 },
    });
    const marks = [mark("first", 0, "第一条"), mark("last", 99, "最后一条")];
    const view = render(
      <Rail
        marks={marks} total={100}
        scroll={{ current: root }} flow={{ current: flow }}
        onJump={vi.fn()} bound
      />,
    );

    const buttons = view.getAllByRole("button");
    await waitFor(() => expect(buttons[0].style.top).toBe("295.5px"));
    expect(buttons[1].style.top).toBe("304.5px");
    expect(buttons[0].getAttribute("aria-label")).toBe("定位到你说的：第一条");
  });
});
