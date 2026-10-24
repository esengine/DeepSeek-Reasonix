// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render } from "@testing-library/react";
import "./testkit";
import { StudioIcon } from "./StudioIcon";
import { ViewSwitch } from "./ViewSwitch";

afterEach(cleanup);

const pathOf = (el: Element | null) => el?.querySelector("path")?.getAttribute("d");

it("draws the run analysis button with the activity pulse, the same mark as its card link", () => {
  const { container } = render(<ViewSwitch view="flow" rows={1} surfaces={0} onView={vi.fn()} />);
  const button = container.querySelector('[data-action="pane.view"][data-value="analysis"]');
  const pulse = render(<StudioIcon name="activity" />).container;
  expect(pathOf(button)).toBeTruthy();
  expect(pathOf(button)).toBe(pathOf(pulse));
});
