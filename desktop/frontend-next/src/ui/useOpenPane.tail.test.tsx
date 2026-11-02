// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { clearToasts, readToasts } from "../state/toasts";
import { HttpError } from "../port/http_error";
import type { RuntimeView } from "../port/hub";
import type { AgentPort } from "../port/port";
import { useOpenPane } from "./useOpenPane";

beforeEach(() => { localStorage.clear(); clearToasts(); });
afterEach(() => clearToasts());

const blank = { id: "r0", root: "/w", name: "w", sessionPath: "" } as RuntimeView;

function window(resume: (path: string) => Promise<void>) {
  const continueFromTail = vi.fn(async (path: string) => ({ path: path.replace(".jsonl", "-tail.jsonl"), dropped: 9 }));
  const panePorts = new Map([["r0", { resume: vi.fn(resume), continueFromTail } as unknown as AgentPort]]);
  const sw = { path: "", pending: false, opening: () => false, begin: () => 1, current: () => true, landed: () => {}, end: () => {}, cancel: () => {} };
  const hook = renderHook(() =>
    useOpenPane({
      hub: { open: vi.fn(), close: vi.fn() } as never, runtimes: [blank], panePorts, active: "r0",
      reportsRef: { current: {} }, runsRef: { current: {} },
      setRuntimes: vi.fn(), setActive: vi.fn(), setTakeover: vi.fn(), reloadPanes: async () => {}, sw,
    }),
  );
  return { continueFromTail, resume: panePorts.get("r0")!.resume as ReturnType<typeof vi.fn>, open: (path: string) => hook.result.current({ root: "/w", sessionPath: path }) };
}

const overLimit = (path: string) => path.endsWith("big.jsonl")
  ? Promise.reject(new HttpError(413, "too large", { code: "session.replay_limit" }))
  : Promise.resolve();

describe("a session whose history is over the safe read limit", () => {
  it("offers its recent part instead of failing, and opens that part as a new session", async () => {
    const w = window(overLimit);
    await act(async () => void (await w.open("/w/big.jsonl")));
    const [offer] = readToasts();
    expect(offer.action?.label).toBe("从最近的内容继续");
    expect(w.continueFromTail).not.toHaveBeenCalled();

    await act(async () => void offer.action?.run());
    expect(w.continueFromTail).toHaveBeenCalledWith("/w/big.jsonl");
    expect(w.resume).toHaveBeenLastCalledWith("/w/big-tail.jsonl");
    expect(readToasts().map((n) => n.text).join("\n")).toContain("9 条没有带入");
  });

  it("still fails for any other refusal", async () => {
    const w = window(() => Promise.reject(new HttpError(400, "bad", { code: "session.bad_path" })));
    await expect(act(async () => void (await w.open("/w/big.jsonl")))).rejects.toBeInstanceOf(HttpError);
    expect(readToasts()).toHaveLength(0);
  });

  it("says why when the recent part cannot be taken either", async () => {
    const w = window(overLimit);
    w.continueFromTail.mockRejectedValueOnce(new HttpError(500, "x", { code: "session.tail_failed" }));
    await act(async () => void (await w.open("/w/big.jsonl")));
    await act(async () => void readToasts()[0].action?.run());
    expect(readToasts().map((n) => n.text).join("\n")).toContain("没能取出这个会话最近的内容");
  });
});
