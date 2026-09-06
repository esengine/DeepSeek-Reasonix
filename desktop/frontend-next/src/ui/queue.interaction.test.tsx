// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Queue } from "./Queue";
import { HttpError } from "../port/port";
import type { Queue as QueueSnapshot, QueueItem } from "../port/port";

afterEach(cleanup);

// The preview is cut at 120 runes, so the box has to be filled from the read
// and never from the row: saving the row back files a truncated line as the
// whole instruction.
const BODY = "把 gate_test.go 里那三个跳过的用例打开，确认它们真的在 CI 里跑到了，再把 README 里说它们被跳过的那段删掉。";

const item = (over: Partial<QueueItem> = {}): QueueItem => ({
  id: "i1",
  intent: "followup",
  state: "queued",
  preview: BODY.slice(0, 20),
  createdAt: "2026-08-25T10:00:00Z",
  ...over,
});

const snapshot = (over: Partial<QueueSnapshot> = {}): QueueSnapshot => ({
  revision: 1,
  paused: false,
  items: [item()],
  capacity: { items: 1, maxItems: 64, bytes: 3174, maxBytes: 64 << 20 },
  ...over,
});

function draw(onRead: (id: string) => Promise<string>) {
  const onEdit = vi.fn();
  render(
    <Queue
      queue={snapshot()}
      onRead={onRead}
      onEdit={onEdit}
      onMove={() => {}}
      onCancel={() => {}}
      onRetry={() => {}}
      onRefresh={() => {}}
      onPause={() => {}}
    />,
  );
  return { onEdit, edit: () => userEvent.click(screen.getByRole("button", { name: "改" })) };
}

const box = () => document.querySelector<HTMLTextAreaElement>(".qedit");

describe("editing a pending entry", () => {
  // The arm the browser guard cannot reach: it needs an inbox_changed to land
  // in the panel, and the bench owns the subscription the fixture emits into.
  it("opens on the whole instruction, not on the row's preview", async () => {
    const { edit } = draw(async () => BODY);
    await edit();
    expect(box()?.value).toBe(BODY);
  });

  it("does not open an editor when the body cannot be read", async () => {
    const { onEdit, edit } = draw(async () => {
      throw new HttpError(409, "no such entry", { code: "inbox.not_found" });
    });
    await edit();
    expect(box()).toBeNull();
    // The row keeps its own text. Anything typed into a blank box would have
    // replaced, in full, a line the reader never saw.
    expect(screen.getByText(item().preview)).toBeTruthy();
    expect(onEdit).not.toHaveBeenCalled();
  });

  // The refusal is the kernel's identity said in this window's language, not
  // the English that rode along for the log.
  it("says why, in the reader's language", async () => {
    const { edit } = draw(async () => {
      throw new HttpError(409, "no such entry", { code: "inbox.not_found" });
    });
    await edit();
    const why = document.querySelector(".qwhy[data-err]");
    expect(why?.textContent).toContain("这一条已经不在待送达里了");
    expect(why?.textContent).not.toContain("no such entry");
  });

  // A failure that outlives its click reads as the next one's answer.
  it("clears the last failure when the read succeeds", async () => {
    let fail = true;
    const { edit } = draw(async () => {
      if (fail) throw new HttpError(409, "gone", { code: "inbox.not_found" });
      return BODY;
    });
    await edit();
    expect(document.querySelector(".qwhy[data-err]")).toBeTruthy();
    fail = false;
    await edit();
    expect(document.querySelector(".qwhy[data-err]")).toBeNull();
    expect(box()?.value).toBe(BODY);
  });
});
