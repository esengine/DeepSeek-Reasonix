// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import "./testkit";
import { UploadUsage } from "./UploadUsage";
import { HttpError, type UploadReport } from "../port/port";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

const report = (over: Partial<UploadReport> = {}): UploadReport => ({
  from: "2026-09-28", to: "2026-10-27", bytes: 3 * 2 ** 30, requests: 1500,
  daily: [{ day: "2026-10-26", bytes: 0 }, { day: "2026-10-27", bytes: 200 * 2 ** 20 }],
  providers: [
    { provider: "deepseek", bytes: 2.7 * 2 ** 30, requests: 1300, percent: 90 },
    { provider: "relay", bytes: 0.3 * 2 ** 30, requests: 200, percent: 10 },
  ],
  ...over,
});

it("says what was uploaded today and in the month, with the average request, and which provider took it", async () => {
  render(<UploadUsage port={{ uploadUsage: async () => report() }} />);
  await screen.findByText("deepseek");
  expect(screen.getByText(/^200(\.0)? MB$/)).toBeTruthy();
  expect(screen.getByText(/^3(\.0)? GB，1,?500 次请求，平均每次 2(\.\d)? MB$/)).toBeTruthy();
  expect(screen.getByText(/2\.7 GB（90%）/)).toBeTruthy();
  expect(screen.getByText("relay")).toBeTruthy();
});

it("asks for the last thirty days", async () => {
  const uploadUsage = vi.fn(async () => report());
  render(<UploadUsage port={{ uploadUsage }} />);
  await screen.findByText("deepseek");
  expect(uploadUsage).toHaveBeenCalledWith(30);
});

it("says counting has only just started instead of drawing zeros", async () => {
  render(<UploadUsage port={{ uploadUsage: async () => report({ bytes: 0, requests: 0, daily: [], providers: [] }) }} />);
  await screen.findByText("还没有记录：从这个版本起开始统计。");
  expect(screen.queryByText("今天上传")).toBeNull();
});

it("shows why it could not be read", async () => {
  render(<UploadUsage port={{ uploadUsage: async () => { throw new HttpError(500, "x", { code: "internal.failed" }); } }} />);
  expect(await screen.findByRole("alert")).toBeTruthy();
});
