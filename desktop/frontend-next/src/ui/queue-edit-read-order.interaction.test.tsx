// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { MockPort } from "../port/mock";
import { HttpError } from "../port/port";
import { Queue } from "./Queue";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

const FIRST = "Check the unit tests.";
const SECOND = "Check the packaged application.";
const DRAFT = "Check the packaged application on Windows as well.";

function deferred() {
  let resolve!: (body: string) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<string>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

async function openQueue() {
  const port = new MockPort();
  await port.setQueuePaused(true);
  const stamp = vi.spyOn(Date, "now").mockReturnValueOnce(1).mockReturnValueOnce(2);
  const first = await port.queueFollowup(FIRST);
  const second = await port.queueFollowup(SECOND);
  stamp.mockRestore();
  expect(first.itemId).not.toBe(second.itemId);
  const reads = new Map([[first.itemId, deferred()], [second.itemId, deferred()]]);
  const edit = vi.spyOn(port, "editQueued");
  render(<Queue queue={await port.queue()} running
    onRead={(id) => reads.get(id)!.promise} onEdit={(id, body) => void port.editQueued(id, body)}
    onMove={() => {}} onCancel={() => {}} onSendNow={() => {}}
    onRetry={() => {}} onRefresh={() => {}} onPause={() => {}} />);
  const choose = async (body: string) => {
    const row = screen.getByText(body).closest(".qi")! as HTMLElement;
    await userEvent.click(within(row).getByRole("button", { name: "改" }));
  };
  await choose(FIRST);
  await choose(SECOND);
  return { port, first, second, earlier: reads.get(first.itemId)!, latest: reads.get(second.itemId)!, edit };
}

const box = () => document.querySelector<HTMLTextAreaElement>(".qedit");

it.each(["body", "refusal"])("keeps the latest queued draft when an earlier read returns a late %s", async (result) => {
  const { port, first, second, earlier, latest, edit } = await openQueue();
  await act(async () => { latest.resolve(await port.readQueued(second.itemId)); });
  expect(box()?.value).toBe(SECOND);
  fireEvent.change(box()!, { target: { value: DRAFT } });
  await act(async () => {
    if (result === "body") earlier.resolve(await port.readQueued(first.itemId));
    else earlier.reject(new HttpError(409, "gone", { code: "inbox.not_found" }));
  });

  expect(box()?.value).toBe(DRAFT);
  expect(document.activeElement).toBe(box());
  expect(screen.queryByRole("alert")).toBeNull();
  fireEvent.keyDown(box()!, { key: "Enter" });
  expect(edit).toHaveBeenCalledExactlyOnceWith(second.itemId, DRAFT);
  expect(await port.readQueued(first.itemId)).toBe(FIRST);
  expect(await port.readQueued(second.itemId)).toBe(DRAFT);
});

it("waits for the latest selected row even when the earlier row answers first", async () => {
  const { port, first, second, earlier, latest } = await openQueue();
  await act(async () => { earlier.resolve(await port.readQueued(first.itemId)); });
  expect(box()).toBeNull();
  await act(async () => { latest.resolve(await port.readQueued(second.itemId)); });
  expect(box()?.value).toBe(SECOND);
  expect(document.activeElement).toBe(box());
});

it("keeps the latest read's refusal after an older body arrives", async () => {
  const { port, first, earlier, latest } = await openQueue();
  await act(async () => { latest.reject(new HttpError(409, "gone", { code: "inbox.not_found" })); });
  const failure = screen.getByRole("alert");
  expect(failure.textContent).toContain("该条已不在待送达队列中");
  expect(box()).toBeNull();
  await act(async () => { earlier.resolve(await port.readQueued(first.itemId)); });
  expect(box()).toBeNull();
  expect(screen.getByRole("alert")).toBe(failure);
});
