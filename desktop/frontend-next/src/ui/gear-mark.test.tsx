// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import { GEAR_PATH, GearMark } from "./GearMark";
import { ICON } from "./prefsnav";

afterEach(cleanup);

const mark = (node: React.ReactNode) => {
  const { container } = render(<svg>{node}</svg>);
  return [...container.querySelectorAll("path,circle")].map((e) => e.getAttribute("d") ?? `circle ${e.getAttribute("r")}`);
};

it("draws settings as a toothed gear, not as the sun the appearance page is", () => {
  expect(GEAR_PATH.split("L").length).toBeGreaterThan(24);
  expect(mark(GearMark)).not.toEqual(mark(ICON.appearance));
  expect(mark(ICON.appearance).some((d) => d.includes("M8 1.5v1.5"))).toBe(true);
});

it("gives the common page the same gear the sidebar's settings button wears", () => {
  expect(mark(ICON.common)).toEqual(mark(GearMark));
});
