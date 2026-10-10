// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Queue } from "./Queue";
import { HttpError } from "../port/port";
import type { Queue as QueueSnapshot, QueueItem } from "../port/port";

afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks(); });

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
      running
      onRead={onRead}
      onEdit={onEdit}
      onMove={() => {}}
      onSendNow={() => {}}
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
    expect(why?.textContent).toContain("该条已不在待送达队列中");
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

describe("retrying a held entry", () => {
  it("asks the kernel to retry exactly that entry", async () => {
    const onRetry = vi.fn();
    render(
      <Queue
        queue={snapshot({ paused: true, items: [item({ id: "u1", state: "uncertain", blockCode: "steer_unapplied" })] })}
        running={false}
        onRead={async () => BODY}
        onEdit={() => {}}
        onMove={() => {}}
        onSendNow={() => {}}
        onCancel={() => {}}
        onRetry={onRetry}
        onRefresh={() => {}}
        onPause={() => {}}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(onRetry).toHaveBeenCalledWith("u1");
  });
});

describe("queue send shortcut", () => {
  it.each([false, true])("preserves Escape propagation when abandoning an edit (composing=%s)", async (composing) => {
    const key = vi.fn();
    window.addEventListener("keydown", key);
    try {
      const { edit, onEdit } = draw(async () => BODY);
      await edit();
      if (composing) fireEvent.compositionStart(box()!);
      expect(fireEvent.keyDown(box()!, { key: "Escape" })).toBe(false);
      expect(key).toHaveBeenCalledTimes(1);
      expect(box()).toBeNull();
      expect(onEdit).not.toHaveBeenCalled();
    } finally { window.removeEventListener("keydown", key); }
  });
  it("keeps Enter for a newline and commits with Control+Enter", async () => {
    localStorage.setItem("rx-send-shortcut", "modifier_enter");
    const { edit, onEdit } = draw(async () => BODY);
    await edit();
    fireEvent.keyDown(box()!, { key: "Enter" });
    expect(onEdit).not.toHaveBeenCalled();
    expect(box()).not.toBeNull();
    fireEvent.keyDown(box()!, { key: "Enter", ctrlKey: true });
    expect(onEdit).toHaveBeenCalledWith("i1", BODY);
    expect(box()).toBeNull();
  });
  it("does not commit during or just after IME composition", async () => {
    const { edit, onEdit } = draw(async () => BODY);
    await edit();
    fireEvent.compositionStart(box()!);
    fireEvent.keyDown(box()!, { key: "Enter", isComposing: true });
    fireEvent.compositionEnd(box()!);
    fireEvent.keyDown(box()!, { key: "Enter" });
    expect(onEdit).not.toHaveBeenCalled();
    expect(box()).not.toBeNull();
  });
});

it.each(["enter", "modifier_enter"])("keeps touch Enter in the queue editor (%s)", async (mode) => {
  const media = window.matchMedia("(pointer: coarse)");
  vi.spyOn(window, "matchMedia").mockReturnValue({ ...media, matches: true });
  localStorage.setItem("rx-send-shortcut", mode);
  const { edit, onEdit } = draw(async () => BODY);
  await edit();
  fireEvent.keyDown(box()!, { key: "Enter", code: "Enter" });
  expect(onEdit).not.toHaveBeenCalled();
  expect(box()).not.toBeNull();
  if (mode === "enter") expect(box()?.hasAttribute("aria-keyshortcuts")).toBe(false);
  if (mode === "modifier_enter") fireEvent.keyDown(box()!, { key: "Enter", code: "Enter", ctrlKey: true });
  else fireEvent.blur(box()!);
  expect(onEdit).toHaveBeenCalledWith("i1", BODY);
});
