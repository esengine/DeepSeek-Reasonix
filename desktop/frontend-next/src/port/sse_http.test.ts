import { afterEach, describe, expect, it, vi } from "vitest";
import { HttpError, KernelBusyError } from "./port";
import { SseHttp } from "./sse_http";

class Probe extends SseHttp {
  call(path: string) {
    return this.post(path, {});
  }
  read<T>(path: string) {
    return this.get<T>(path);
  }
  decide(path: string) {
    return this.postDecision(path, {});
  }
}

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

function deferredFetch() {
  const pending: ((v: unknown) => void)[] = [];
  const fetchMock = vi.fn(
    () =>
      new Promise((resolve) => {
        pending.push((v) => resolve({ ok: true, status: 200, json: async () => v, text: async () => "" }));
      }),
  );
  vi.stubGlobal("fetch", fetchMock);
  return { fetchMock, pending };
}

const tick = () => new Promise((r) => setTimeout(r, 0));

describe("idempotent reads", () => {
  it("share one request while it is in flight, with one trailing re-read for the rest", async () => {
    const { fetchMock, pending } = deferredFetch();
    const probe = new Probe();
    const first = probe.read("/status");
    const second = probe.read("/status");
    const third = probe.read("/status");
    expect(fetchMock).toHaveBeenCalledTimes(1);
    pending[0]({ n: 1 });
    expect(await first).toEqual({ n: 1 });
    await tick();
    expect(fetchMock).toHaveBeenCalledTimes(2);
    pending[1]({ n: 2 });
    expect(await second).toEqual({ n: 2 });
    expect(await third).toEqual({ n: 2 });
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("never hands a trigger an answer that began before it", async () => {
    const { pending } = deferredFetch();
    const probe = new Probe();
    const early = probe.read<{ n: number }>("/status");
    const late = probe.read<{ n: number }>("/status");
    pending[0]({ n: 1 });
    await early;
    await tick();
    pending[1]({ n: 2 });
    expect((await late).n).toBe(2);
  });

  it("keeps different paths apart", async () => {
    const { fetchMock } = deferredFetch();
    const probe = new Probe();
    void probe.read("/status");
    void probe.read("/context");
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("does not let a failed read poison the next one", async () => {
    let n = 0;
    const fetchMock = vi.fn(async () => {
      n++;
      if (n === 1) throw new TypeError("Failed to fetch");
      return { ok: true, status: 200, json: async () => ({ n }), text: async () => "" };
    });
    vi.stubGlobal("fetch", fetchMock);
    const probe = new Probe();
    await expect(probe.read("/status")).rejects.toBeInstanceOf(TypeError);
    expect(await probe.read("/status")).toEqual({ n: 2 });
  });
});

describe("a decision submit", () => {
  it("fails with a typed error instead of waiting forever", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("fetch", (_: string, init: RequestInit) =>
      new Promise((_resolve, reject) => {
        init.signal?.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")));
      }),
    );
    const settled = new Probe().decide("/approve").catch((e) => e);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(await settled).toBeInstanceOf(KernelBusyError);
  });

  it("keeps a refusal it already received when the wait runs out mid-body", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("fetch", async (_: string, init: RequestInit) => ({
      ok: false,
      status: 409,
      text: () =>
        new Promise<string>((_resolve, reject) => {
          init.signal?.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")));
        }),
    }));
    const settled = new Probe().decide("/plan-decision").catch((e) => e);
    await vi.advanceTimersByTimeAsync(60_000);
    const err = await settled;
    expect(err).not.toBeInstanceOf(KernelBusyError);
    expect((err as HttpError).status).toBe(409);
  });

  it("goes through untouched when the kernel answers", async () => {
    vi.stubGlobal("fetch", async () => ({ ok: true, status: 204, text: async () => "" }));
    await expect(new Probe().decide("/approve")).resolves.toBeUndefined();
  });

  it("still carries a refusal's code", async () => {
    answer(409, JSON.stringify({ code: "gate.stale", error: "stale" }), "application/json");
    const err = (await new Probe().decide("/approve").catch((e) => e)) as HttpError;
    expect(err.reason?.code).toBe("gate.stale");
  });
});

function answer(status: number, body: string, contentType: string) {
  vi.stubGlobal("fetch", async () => ({
    ok: false,
    status,
    text: async () => body,
    json: async () => JSON.parse(body),
    headers: new Map([["content-type", contentType]]),
  }));
}

describe("a failed call", () => {
  // http.Error writes plain text, and parsing the body as JSON first threw the
  // only account of the failure away: users reported "/providers/edit: 500".
  it("carries a plain-text reason through", async () => {
    answer(500, "save user config: config is locked by another Reasonix process", "text/plain");
    const err = (await new Probe().call("/providers/edit").catch((e) => e)) as HttpError;
    expect(err).toBeInstanceOf(HttpError);
    expect(err.status).toBe(500);
    expect(err.message).toContain("locked by another Reasonix process");
  });

  it("still prefers the refusal envelope when there is one", async () => {
    answer(400, JSON.stringify({ code: "provider.no_models_picked", error: "pick at least one model" }), "application/json");
    const err = (await new Probe().call("/providers/edit").catch((e) => e)) as HttpError;
    expect(err.message).toBe("pick at least one model");
    expect(err.reason?.code).toBe("provider.no_models_picked");
  });

  // With nothing to report, the path and status are all a reader can be given.
  it("falls back to the path and status on an empty body", async () => {
    answer(502, "", "text/plain");
    const err = (await new Probe().call("/providers/edit").catch((e) => e)) as HttpError;
    expect(err.message).toBe("/providers/edit: 502");
  });
});
