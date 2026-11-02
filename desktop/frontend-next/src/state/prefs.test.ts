// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { liveGraceMinutes, LIVE_GRACE_CHOICES, onLiveGraceChange, onOpenSessionLimitChange, openSessionLimit, runningMessageIntent, setLiveGraceMinutes, setOpenSessionLimit, setRunningMessageIntent, onPlanProgressChange, setShowsPlanProgress, showsPlanProgress } from "./prefs";

beforeEach(() => localStorage.clear());
afterEach(() => { vi.restoreAllMocks(); setRunningMessageIntent("steer"); localStorage.clear(); });

describe("default intent for messages sent during a run", () => {
  it("defaults to steering and reads only the offered follow-up choice", () => {
    expect(runningMessageIntent()).toBe("steer");
    for (const raw of ["", "queued", "invalid"]) {
      localStorage.setItem("rx-running-message-intent", raw);
      expect(runningMessageIntent()).toBe("steer");
    }
    setRunningMessageIntent("followup");
    expect(runningMessageIntent()).toBe("followup");
    expect(localStorage.getItem("rx-running-message-intent")).toBe("followup");
  });

  it("holds the choice for this window if storage cannot be written", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("unavailable"); });
    setRunningMessageIntent("followup");
    expect(runningMessageIntent()).toBe("followup");
  });
});

describe("collapsed plan progress on this machine", () => {
  it("is off by default and only accepts an explicit on preference", () => {
    expect(showsPlanProgress()).toBe(false);
    for (const raw of ["", "true", "off", "garbage"]) {
      localStorage.setItem("rx-plan-progress", raw);
      expect(showsPlanProgress()).toBe(false);
    }
    setShowsPlanProgress(true);
    expect(showsPlanProgress()).toBe(true);
    expect(localStorage.getItem("rx-plan-progress")).toBe("on");
    setShowsPlanProgress(false);
    expect(showsPlanProgress()).toBe(false);
  });

  it("notifies mounted subscribers and removes them on unsubscribe", () => {
    const heard = vi.fn();
    const stop = onPlanProgressChange(heard);
    setShowsPlanProgress(true);
    stop();
    setShowsPlanProgress(false);
    expect(heard).toHaveBeenCalledTimes(1);
  });

  it("stays usable with unavailable storage", () => {
    const get = vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("unavailable"); });
    const set = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("unavailable"); });
    try {
      expect(showsPlanProgress()).toBe(false);
      expect(() => setShowsPlanProgress(true)).not.toThrow();
      expect(showsPlanProgress()).toBe(false);
    } finally {
      get.mockRestore();
      set.mockRestore();
    }
  });
});

describe("how long a finished session stays in the running list", () => {
  it("is off until this machine chooses otherwise", () => {
    expect(liveGraceMinutes()).toBe(0);
  });

  it("reads back a choice from the offered list", () => {
    for (const minutes of LIVE_GRACE_CHOICES) {
      setLiveGraceMinutes(minutes);
      expect(liveGraceMinutes()).toBe(minutes);
    }
  });

  it("treats anything unreadable, unoffered or negative as off", () => {
    for (const raw of ["", "abc", "-5", "7", "1e3", "30 "]) {
      localStorage.setItem("rx-live-grace", raw);
      expect(liveGraceMinutes(), raw).toBe(0);
    }
  });

  it("tells subscribers when the choice changes", () => {
    let heard = 0;
    const stop = onLiveGraceChange(() => heard++);
    setLiveGraceMinutes(30);
    stop();
    setLiveGraceMinutes(60);
    expect(heard).toBe(1);
  });
});

describe("how many sessions stay open as tabs", () => {
  it("never folds one on its own until this machine chooses a number", () => {
    expect(openSessionLimit()).toBe(0);
  });

  it("reads back any non-negative whole number, without an upper bound", () => {
    for (const n of [0, 1, 4, 12, 40, 1000]) {
      setOpenSessionLimit(n);
      expect(openSessionLimit(), String(n)).toBe(n);
    }
  });

  it("treats anything missing, unreadable, fractional or negative as never folding", () => {
    for (const raw of ["", "abc", "-3", "2.5", "1e3", "4 ", " 4", "NaN", "Infinity", "0x10", "9007199254740993"]) {
      localStorage.setItem("rx-open-session-limit", raw);
      expect(openSessionLimit(), raw).toBe(0);
    }
    localStorage.removeItem("rx-open-session-limit");
    expect(openSessionLimit()).toBe(0);
  });

  it("refuses to store a choice that is not a non-negative whole number", () => {
    setOpenSessionLimit(6);
    for (const bad of [-1, 1.5, Number.NaN, Number.POSITIVE_INFINITY]) {
      setOpenSessionLimit(bad);
      expect(openSessionLimit(), String(bad)).toBe(0);
    }
  });

  it("tells subscribers when the choice changes", () => {
    let heard = 0;
    const stop = onOpenSessionLimitChange(() => heard++);
    setOpenSessionLimit(8);
    stop();
    setOpenSessionLimit(4);
    expect(heard).toBe(1);
  });
});
