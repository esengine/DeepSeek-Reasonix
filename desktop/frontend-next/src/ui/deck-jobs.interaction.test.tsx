// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import "./testkit";
import { DeckChips, type Deck } from "./DeckChips";
import type { JobEntry } from "../port/port";

afterEach(cleanup);

const JOBS: JobEntry[] = [
  { id: "j1", kind: "bash", label: "npm run tauri:test", status: "running", startedAt: Date.now() - 5000 },
  { id: "j2", kind: "bash", label: "go build", status: "exited", startedAt: Date.now() - 9000 },
];

function Host({ onCancelJob }: { onCancelJob: (id: string) => Promise<void> }) {
  const [open, setOpen] = useState<Deck>("");
  return (
    <div>
      <p>outside</p>
      <DeckChips tasks={[]} jobs={JOBS} open={open} onOpen={(next) => setOpen(next)} onCancelJob={onCancelJob} />
    </div>
  );
}

const chip = () => document.querySelector<HTMLButtonElement>('[data-action="deck.jobs"]')!;

it("stops a running job from the deck, and offers no stop for one that ended", async () => {
  const onCancelJob = vi.fn().mockResolvedValue(undefined);
  render(<Host onCancelJob={onCancelJob} />);
  await userEvent.click(chip());
  expect(screen.queryByRole("button", { name: "停止后台任务：go build" })).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: "停止后台任务：npm run tauri:test" }));
  expect(onCancelJob).toHaveBeenCalledWith("j1");
});

it("closes on a press outside and on Escape, not only on its own chip", async () => {
  render(<Host onCancelJob={async () => {}} />);
  await userEvent.click(chip());
  expect(chip().getAttribute("aria-expanded")).toBe("true");
  await userEvent.click(screen.getByText("outside"));
  expect(chip().getAttribute("aria-expanded")).toBe("false");
  await userEvent.click(chip());
  await userEvent.keyboard("{Escape}");
  expect(chip().getAttribute("aria-expanded")).toBe("false");
});

it("reads a stopped job as stopped, not as a success", async () => {
  const { Jobs } = await import("./panels/Jobs");
  const ended = ["done", "failed", "killed", "interrupted"].map((status, i) => ({ id: `e${i}`, kind: "bash", label: status, status, startedAt: 0 }));
  const { container } = render(<Jobs jobs={ended} />);
  const row = (status: string) => container.querySelector<HTMLElement>(`.job[data-status="${status}"]`)!;
  const tone = (status: string) => row(status).querySelector<HTMLElement>(".pip")!.style.background;
  expect(row("killed").querySelector(".rt")!.textContent).toBe("已停止");
  expect(tone("done")).toBe("var(--ok)");
  expect(tone("failed")).toBe("var(--err)");
  expect(tone("killed")).toBe("var(--faint)");
  expect(tone("interrupted")).toBe("var(--faint)");
});
