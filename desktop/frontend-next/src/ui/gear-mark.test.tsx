// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import { StudioIcon } from "./StudioIcon";
import { ICON } from "./prefsnav";

afterEach(cleanup);

const mark = (node: React.ReactNode) => {
  return render(<>{node}</>).container.querySelector("svg")?.getAttribute("class");
};

it("distinguishes settings and appearance with their library icons", () => {
  expect(mark(<StudioIcon name="settings" />)).toContain("lucide-settings");
  expect(mark(ICON.appearance)).toContain("lucide-palette");
});

it("gives the common page the same gear the sidebar's settings button wears", () => {
  expect(mark(ICON.common)).toEqual(mark(<StudioIcon name="settings" />));
});
