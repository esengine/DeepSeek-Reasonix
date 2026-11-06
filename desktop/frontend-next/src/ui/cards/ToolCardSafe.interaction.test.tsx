// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render } from "@testing-library/react";
import "../testkit";
import { ToolCard } from "./ToolCardSafe";

vi.mock("./ToolCard", () => ({
  ToolCard: () => { throw new Error("card failed"); },
}));

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

const draw = (args: string | undefined) => {
  vi.spyOn(console, "warn").mockImplementation(() => {});
  return render(<ToolCard tool={{ name: "edit_file", args, readOnly: false }} running={false} />).container;
};

describe("a tool card that cannot be drawn", () => {
  it("falls back to its name and target on one line, tagged as simplified", () => {
    const c = draw(JSON.stringify({ path: "internal/retry.go", old: "a" }));
    expect(c.querySelector(".toolfb-nm")?.textContent).toBe("edit_file · internal/retry.go");
    expect(c.querySelector(".srcfb-tag")?.textContent).toBe("简化显示");
  });

  it("names only the call when its arguments do not parse", () => {
    const c = draw("{not json");
    expect(c.querySelector(".toolfb-nm")?.textContent).toBe("edit_file");
  });

  it("names only the call when it was given nothing", () => {
    expect(draw(undefined).querySelector(".toolfb-nm")?.textContent).toBe("edit_file");
  });
});
