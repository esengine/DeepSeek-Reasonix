// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";

afterEach(cleanup);

const BODY = "Check the failing tests after the current task.";

async function openWindow() {
  const hub = new MockHub();
  const [rt] = await hub.runtimes();
  const port = hub.portFor(rt);
  port.providerSetup = async () => null;
  port.welcomeSeen = async () => true;
  const status = await port.status();
  vi.spyOn(port, "status").mockResolvedValue({ ...status, running: true });
  const cancel = vi.spyOn(port, "cancel").mockResolvedValue(undefined);
  const edit = vi.spyOn(port, "editQueued");
  await port.setQueuePaused(true);
  const queued = await port.queueFollowup(BODY);
  await act(async () => { render(<App hub={hub} />); });
  await screen.findByRole("button", { name: "改" });
  await waitFor(() => expect(document.querySelector(".studio-runstate[data-running]")).not.toBeNull());
  return { port, queued, cancel, edit };
}

async function openEditor() {
  await userEvent.click(screen.getByRole("button", { name: "改" }));
  return waitFor(() => {
    const box = document.querySelector<HTMLTextAreaElement>(".qedit");
    expect(box).not.toBeNull();
    expect(document.activeElement).toBe(box);
    return box!;
  });
}

it("dismisses a queued draft with Escape without stopping the active turn", async () => {
  const { port, queued, cancel, edit } = await openWindow();
  const box = await openEditor();
  expect(box.value).toBe(BODY);
  fireEvent.change(box, { target: { value: "An unsaved replacement" } });
  await userEvent.keyboard("{Escape}");

  expect(document.querySelector(".qedit")).toBeNull();
  expect(cancel).not.toHaveBeenCalled();
  expect(edit).not.toHaveBeenCalled();
  expect(await port.readQueued(queued.itemId)).toBe(BODY);
  expect((await port.queue()).items.map((item) => item.id)).toContain(queued.itemId);

  const reopened = await openEditor();
  expect(reopened.value).toBe(BODY);
  await userEvent.keyboard("{Escape}");
  expect(cancel).not.toHaveBeenCalled();

  const composer = document.querySelector<HTMLTextAreaElement>(".compose textarea")!;
  expect(composer).not.toBeNull();
  composer.focus();
  await userEvent.keyboard("{Escape}");
  expect(cancel).toHaveBeenCalledTimes(1);
});

it("still saves a queued draft with Enter without stopping the active turn", async () => {
  const { port, queued, cancel, edit } = await openWindow();
  const box = await openEditor();
  const replacement = "Check the build as well.";
  fireEvent.change(box, { target: { value: replacement } });
  await userEvent.keyboard("{Enter}");

  await waitFor(() => expect(document.querySelector(".qedit")).toBeNull());
  expect(edit).toHaveBeenCalledExactlyOnceWith(queued.itemId, replacement);
  expect(await port.readQueued(queued.itemId)).toBe(replacement);
  expect(cancel).not.toHaveBeenCalled();
});
