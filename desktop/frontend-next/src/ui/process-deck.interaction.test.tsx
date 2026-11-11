// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import "./testkit";
import { DeckChips, type Deck } from "./DeckChips";
import type { JobEntry } from "../port/port";
import type { ProcessEntry } from "../port/process";
import { HttpError } from "../port/http_error";

afterEach(cleanup);

const JOBS: JobEntry[] = [{ id: "j1", kind: "bash", label: "npm run build", status: "running", startedAt: Date.now() - 5000 }];

const ROWS: ProcessEntry[] = [
  { id: "job:j1", kind: "job", name: "npm run build", state: "running", startedAt: Date.now() - 5000 },
  { id: "subagent:sa_1", kind: "subagent", parent: "turn:t3", name: "audit", state: "queued", cause: "writers", startedAt: Date.now() - 2000 },
];

function Host({ read, jobs = JOBS }: { read?: () => Promise<ProcessEntry[]>; jobs?: JobEntry[] }) {
  const [open, setOpen] = useState<Deck>("");
  return <DeckChips tasks={[]} jobs={jobs} open={open} onOpen={(next) => setOpen(next)} onReadProcesses={read} />;
}

const chip = () => document.querySelector<HTMLButtonElement>('[data-action="deck.processes"]')!;

it("reads nothing until the list is opened", async () => {
  const read = vi.fn().mockResolvedValue(ROWS);
  render(<Host read={read} />);
  await act(async () => { await Promise.resolve(); });
  expect(read).not.toHaveBeenCalled();
  await userEvent.click(chip());
  await waitFor(() => expect(read).toHaveBeenCalledTimes(1));
});

it("shows each process with its state and the cause the kernel gave", async () => {
  render(<Host read={vi.fn().mockResolvedValue(ROWS)} />);
  await userEvent.click(chip());
  const dialog = await screen.findByRole("dialog", { name: "进程" });
  expect(dialog.textContent).toContain("npm run build");
  expect(dialog.textContent).toContain("运行中");
  expect(dialog.textContent).toContain("audit");
  expect(dialog.textContent).toContain("排队中");
  expect(dialog.textContent).toContain("等写入名额");
});

it("does not poll while open, and reads again when the jobs it was built from change", async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  try {
    const read = vi.fn().mockResolvedValue(ROWS);
    const { rerender } = render(<Host read={read} />);
    await userEvent.click(chip());
    await waitFor(() => expect(read).toHaveBeenCalledTimes(1));
    await act(async () => { vi.advanceTimersByTime(120_000); });
    expect(read).toHaveBeenCalledTimes(1);
    rerender(<Host read={read} jobs={[...JOBS, { id: "j2", kind: "bash", label: "go test", status: "running", startedAt: Date.now() }]} />);
    await waitFor(() => expect(read).toHaveBeenCalledTimes(2));
  } finally { vi.useRealTimers(); }
});

it("says so when the read fails and keeps no stale rows", async () => {
  const read = vi.fn().mockResolvedValueOnce(ROWS).mockRejectedValueOnce(new HttpError(500, "disk gone", { code: "process.read_failed", params: { detail: "disk gone" } }));
  render(<Host read={read} />);
  await userEvent.click(chip());
  await screen.findByText("audit");
  await userEvent.click(chip());
  await userEvent.click(chip());
  await waitFor(() => expect(screen.queryByText("audit")).toBeNull());
  expect(screen.getByRole("alert").textContent).toContain("读取进程列表失败：disk gone");
});

it("offers no process chip where the window cannot read processes", () => {
  render(<Host />);
  expect(chip()).toBeNull();
});
