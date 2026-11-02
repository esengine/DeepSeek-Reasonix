// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import "./testkit";
import { SessionAge } from "./SessionAge";

afterEach(cleanup);

it.each([undefined, 0, NaN, Infinity, 1_700_000_000_000])("renders exactly one unread mark instead of the date for %s", (at) => {
  const { container, rerender } = render(<SessionAge at={at} unread />);
  expect(screen.getAllByRole("img", { name: "未读" })).toHaveLength(1);
  expect(container.querySelector("time")).toBeNull();
  rerender(<SessionAge at={at} unread={false} />);
  expect(screen.queryByRole("img")).toBeNull();
  expect(container.querySelectorAll("time")).toHaveLength(at && Number.isFinite(at) ? 1 : 0);
});

it("restores the original date when the canonical unread field clears", () => {
  const at = 1_700_000_000_000;
  const { container, rerender } = render(<SessionAge at={at} unread />);
  rerender(<SessionAge at={at} />);
  expect(container.querySelector("time")?.dateTime).toBe(new Date(at).toISOString());
  expect(screen.queryByRole("img")).toBeNull();
});
