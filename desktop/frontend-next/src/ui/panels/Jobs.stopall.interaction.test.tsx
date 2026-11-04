// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../testkit";
import { Jobs } from "./Jobs";
import type { JobEntry } from "../../port/port";

afterEach(cleanup);

const job = (id: string, status: string): JobEntry => ({ id, label: `cmd ${id}`, status, startedAt: Date.now() }) as JobEntry;

it("offers stopping all only when two or more are running, and asks once", async () => {
  const cancel = vi.fn((_id: string) => Promise.resolve());
  const { rerender } = render(<Jobs jobs={[job("a", "running")]} onCancel={cancel} />);
  expect(document.querySelector('[data-action="job.arm-all"]')).toBeNull();
  rerender(<Jobs jobs={[job("a", "running"), job("b", "running"), job("c", "done")]} onCancel={cancel} />);
  await userEvent.click(document.querySelector('[data-action="job.arm-all"]')!);
  expect(cancel).not.toHaveBeenCalled();
  await userEvent.click(document.querySelector('[data-action="job.cancel-all"]')!);
  expect(cancel.mock.calls.map((c) => c[0])).toEqual(["a", "b"]);
});

it("does not stop anything when the confirmation times out", async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  const cancel = vi.fn((_id: string) => Promise.resolve());
  render(<Jobs jobs={[job("a", "running"), job("b", "running")]} onCancel={cancel} />);
  await userEvent.click(document.querySelector('[data-action="job.arm-all"]')!);
  vi.advanceTimersByTime(3100);
  expect(await screen.findByText("全部停止")).toBeTruthy();
  expect(document.querySelector('[data-action="job.cancel-all"]')).toBeNull();
  expect(cancel).not.toHaveBeenCalled();
  vi.useRealTimers();
});
