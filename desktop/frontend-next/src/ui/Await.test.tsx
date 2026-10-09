// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import "./testkit";
import { Await } from "./Await";
import { PaneShown } from "./shown";
import { boot, STORAGE } from "../i18n";
import { retryPhase } from "../state/retry_line";
import type { Waiting } from "../state/session";

type Retry = NonNullable<Waiting["retry"]>;

const NOW = 1_700_000_000_000;

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(NOW);
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  localStorage.removeItem(STORAGE);
  boot();
});

function line(retry: Retry | undefined, lang: "zh" | "en", advanceMs = 0): string {
  localStorage.setItem(STORAGE, lang);
  boot();
  const { container } = render(<Await since={NOW} retry={retry} />);
  act(() => {
    vi.advanceTimersByTime(advanceMs);
  });
  return container.querySelector(".t")?.textContent ?? "";
}

const retry = (over: Partial<Retry> = {}): Retry => ({ attempt: 1, max: 10, scope: "headers", since: NOW, ...over });

describe("retryPhase", () => {
  it("is a backoff until the delay has elapsed, then a wait that restarts from zero", () => {
    const r = retry({ delayMs: 4000 });
    expect(retryPhase(r, 0)).toEqual({ kind: "backoff", nextSecs: 4 });
    expect(retryPhase(r, 1500)).toEqual({ kind: "backoff", nextSecs: 3 });
    expect(retryPhase(r, 3999)).toEqual({ kind: "backoff", nextSecs: 1 });
    expect(retryPhase(r, 4000)).toEqual({ kind: "waiting", secs: 0 });
    expect(retryPhase(r, 117_900)).toEqual({ kind: "waiting", secs: 113.9 });
  });

  it("has no backoff when the kernel sent none", () => {
    expect(retryPhase(retry(), 2500)).toEqual({ kind: "waiting", secs: 2.5 });
  });
});

describe("the retry line", () => {
  it("says what it waits for and when that counts as no answer", () => {
    const text = line(retry({ cause: "timeout", timeoutSecs: 300, delayMs: 500 }), "en", 114_400);
    expect(text).toBe("The server did not answer in time · Retry 1/10 · waiting for the server to answer 113.9 s (300 s counts as no answer)");
  });

  it("says the same in Chinese", () => {
    const text = line(retry({ cause: "timeout", timeoutSecs: 300, delayMs: 500 }), "zh", 114_400);
    expect(text).toBe("服务器没有及时响应 · 重试 1/10 · 等待服务器响应 113.9s（超过 300s 视为无响应）");
  });

  it("counts down to the next retry during a backoff", () => {
    expect(line(retry({ cause: "connection_closed", delayMs: 4000 }), "en", 100)).toBe(
      "Connection closed · next retry (1/10) in 4 s",
    );
    cleanup();
    expect(line(retry({ cause: "connection_closed", delayMs: 4000 }), "zh", 2100)).toBe("连接被中断 · 2s 后重试 1/10");
  });

  it("omits the no-answer bound when the kernel did not send one", () => {
    expect(line(retry({ cause: "connection_closed" }), "en", 2000)).toBe(
      "Connection closed · Retry 1/10 · waiting for the server to answer 2.0 s",
    );
  });

  it("names an upstream status by its code, never by guessing prose", () => {
    for (const status of [502, 429, 503]) {
      cleanup();
      const text = line(retry({ cause: "upstream_status", status, delayMs: 1000 }), "en", 0);
      expect(text).toContain(`HTTP ${status}`);
      expect(text).not.toContain("dropped");
      expect(text).not.toContain("connection");
    }
  });

  it("does not call a status a broken connection in Chinese either", () => {
    const text = line(retry({ cause: "upstream_status", status: 502, delayMs: 1000 }), "zh", 0);
    expect(text).toContain("HTTP 502");
    expect(text).not.toContain("连接");
  });

  it("words a stream replay as a replay", () => {
    expect(line(retry({ scope: "stream", cause: "stream_idle", delayMs: 1000 }), "en", 0)).toBe(
      "The response stalled · next replay (1/10) in 1 s",
    );
    cleanup();
    expect(line(retry({ scope: "stream", cause: "upstream_error" }), "en", 1000)).toBe(
      "The server reported an error mid-response · Replay 1/10 · waiting for the server to answer 1.0 s",
    );
  });

  it("falls back to a neutral lead when no cause came with the event", () => {
    expect(line(retry({ attempt: 2 }), "en", 1000)).toBe(
      "The request did not succeed · Retry 2/10 · waiting for the server to answer 1.0 s",
    );
  });

  it("restarts the clock when the next attempt's notice arrives", () => {
    localStorage.setItem(STORAGE, "en");
    boot();
    const first = retry({ cause: "timeout", timeoutSecs: 300, delayMs: 500 });
    const { container, rerender } = render(<Await since={NOW} retry={first} />);
    act(() => {
      vi.advanceTimersByTime(60_500);
    });
    expect(container.querySelector(".t")?.textContent).toContain("60.0 s");
    rerender(<Await since={NOW} retry={{ ...first, attempt: 2, since: NOW + 60_500 }} />);
    act(() => {
      vi.advanceTimersByTime(600);
    });
    expect(container.querySelector(".t")?.textContent).toContain("Retry 2/10");
    expect(container.querySelector(".t")?.textContent).toContain("waiting for the server to answer 0.1 s");
  });

  it("still shows the plain wait when nothing is being retried", () => {
    expect(line(undefined, "en", 1500)).toBe("Waiting for a response · 1.5s");
  });
});

describe("the wait clock in a pane nobody is looking at", () => {
  it("does not tick, and reads the true time when brought forward", () => {
    const view = (shown: boolean) => (
      <PaneShown.Provider value={shown}>
        <Await since={NOW} />
      </PaneShown.Provider>
    );
    localStorage.setItem(STORAGE, "zh");
    boot();
    const tick = vi.spyOn(window, "setInterval");
    const { container, rerender } = render(view(false));
    act(() => void vi.advanceTimersByTime(3000));
    expect(tick).not.toHaveBeenCalled();
    expect(container.querySelector(".t")?.textContent).toBe("等待回包 0.0s");
    rerender(view(true));
    expect(container.querySelector(".t")?.textContent).toBe("等待回包 3.0s");
    act(() => void vi.advanceTimersByTime(500));
    expect(container.querySelector(".t")?.textContent).toBe("等待回包 3.5s");
  });
});
