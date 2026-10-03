// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, renderHook } from "@testing-library/react";
import { MockPort } from "../port/mock";
import { HttpError } from "../port/port";
import { useSubmitActions } from "./submit";

afterEach(cleanup);

function setup(running = false) {
  const port = new MockPort();
  const send = vi.spyOn(port, "submit").mockResolvedValue(undefined);
  const steer = vi.spyOn(port, "steer").mockResolvedValue({ itemId: "steer-1", disposition: "queued_steer" });
  const queue = vi.spyOn(port, "queueFollowup").mockResolvedValue({ itemId: "followup-1", disposition: "queued_followup" });
  const dispatch = vi.fn();
  const trajDispatch = vi.fn();
  const refreshStatus = vi.fn();
  const fail = vi.fn();
  const view = renderHook(({ running }) => useSubmitActions({
    port, running, dispatch, trajDispatch, refreshStatus, fail,
  }), { initialProps: { running } });
  const submit = async () => {
    let accepted = false;
    await act(async () => { accepted = await view.result.current.submit("hello"); });
    return accepted;
  };
  return { ...view, send, steer, queue, dispatch, trajDispatch, refreshStatus, fail, submit };
}

it("sends idle input and refreshes status", async () => {
  const s = setup();
  expect(await s.submit()).toBe(true);
  expect(s.send).toHaveBeenCalledWith("hello", undefined);
  expect(s.dispatch).toHaveBeenCalledWith(expect.objectContaining({ kind: "__user", text: "hello", pending: false }));
  expect(s.trajDispatch).toHaveBeenCalledWith({ kind: "__user", text: "hello" });
  expect(s.refreshStatus).toHaveBeenCalledOnce();
});

it("uses the latest running state to steer input", async () => {
  const s = setup();
  s.rerender({ running: true });
  expect(await s.submit()).toBe(true);
  expect(s.send).not.toHaveBeenCalled();
  expect(s.steer).toHaveBeenCalledWith("hello");
  expect(s.dispatch).toHaveBeenCalledWith(expect.objectContaining({ kind: "__queued", itemId: "steer-1", queued: "steer" }));
});

it("queues input if an idle submit races with a running turn", async () => {
  const s = setup();
  s.send.mockRejectedValue(new HttpError(409, "busy", { code: "busy.session_running" }));
  expect(await s.submit()).toBe(true);
  expect(s.queue).toHaveBeenCalledWith("hello", undefined);
  expect(s.dispatch).toHaveBeenCalledWith(expect.objectContaining({ kind: "__queued", itemId: "followup-1", queued: "followup" }));
  expect(s.fail).not.toHaveBeenCalled();
});

it("marks failed input unsent instead of queueing an unrelated error", async () => {
  const s = setup();
  const error = new Error("send failed");
  s.send.mockRejectedValue(error);
  expect(await s.submit()).toBe(false);
  expect(s.dispatch).toHaveBeenLastCalledWith(expect.objectContaining({ kind: "__unsent" }));
  expect(s.fail).toHaveBeenCalledWith(error);
  expect(s.queue).not.toHaveBeenCalled();
});
