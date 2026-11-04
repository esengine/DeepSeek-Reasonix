// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import "./testkit";
import { ageWord, SessionAge } from "./SessionAge";

afterEach(cleanup);

it.each([undefined, 0, NaN, Infinity, 1_700_000_000_000])("marks an unread row with one dot and keeps the date beside it for %s", (at) => {
  const { container, rerender } = render(<SessionAge at={at} unread />);
  expect(screen.getAllByRole("img", { name: "未读" })).toHaveLength(1);
  expect(container.querySelectorAll("time")).toHaveLength(at && Number.isFinite(at) ? 1 : 0);
  rerender(<SessionAge at={at} unread={false} />);
  expect(screen.queryByRole("img")).toBeNull();
  expect(container.querySelectorAll("time")).toHaveLength(at && Number.isFinite(at) ? 1 : 0);
});

it("restores the plain date when the canonical unread field clears", () => {
  const at = 1_700_000_000_000;
  const { container, rerender } = render(<SessionAge at={at} unread />);
  rerender(<SessionAge at={at} />);
  expect(container.querySelector("time")?.dateTime).toBe(new Date(at).toISOString());
  expect(screen.queryByRole("img")).toBeNull();
});

it("writes an age the way the design does", () => {
  const now = new Date(2026, 10, 3, 15, 0, 0).getTime();
  expect(ageWord(now - 20_000, now)).toBe("刚刚");
  expect(ageWord(now - 5 * 60_000, now)).toBe("5 分钟");
  expect(ageWord(now - 2 * 3_600_000, now)).toBe("2 小时");
  expect(ageWord(new Date(2026, 10, 2, 9, 0, 0).getTime(), now)).toBe("昨天");
  expect(ageWord(new Date(2026, 9, 30, 9, 0, 0).getTime(), now)).toBe("10-30");
});
