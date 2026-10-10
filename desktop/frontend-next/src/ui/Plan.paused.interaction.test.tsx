// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import { Plan } from "./Plan";

afterEach(cleanup);

const steps = [
  { text: "one", status: "completed" },
  { text: "two", status: "in_progress" },
] as never;

it("marks the plan paused while the turn waits on a person", () => {
  const { container, rerender } = render(<Plan steps={steps} />);
  expect(container.querySelector(".plan")?.hasAttribute("data-paused")).toBe(false);
  rerender(<Plan steps={steps} paused />);
  expect(container.querySelector(".plan")?.hasAttribute("data-paused")).toBe(true);
});
