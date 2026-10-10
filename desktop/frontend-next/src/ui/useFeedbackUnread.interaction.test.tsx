// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import { StrictMode, useState } from "react";
import { useFeedbackUnread, FOCUS_GAP_MS, POLL_CEILING_MS, POLL_MS } from "./useFeedbackUnread";
import type { AgentPort, NotifyPrefs } from "../port/port";
import type { FeedbackMine, FeedbackStatus } from "../port/feedback";

const mine = (unread: number, statuses: FeedbackStatus[]): FeedbackMine =>
  ({ items: statuses.map((status, i) => ({ receipt: `FB-${i}`, status })), unread, hasNew: unread > 0, offline: false }) as unknown as FeedbackMine;

type Faked = AgentPort & { myFeedback: ReturnType<typeof vi.fn>; announceFeedbackReply: ReturnType<typeof vi.fn> };
const portOf = (ask: () => Promise<FeedbackMine>, prefs: NotifyPrefs | null = null) =>
  ({
    myFeedback: vi.fn(ask),
    announceFeedbackReply: vi.fn(async () => {}),
    notifyPrefs: vi.fn(async () => prefs),
  }) as unknown as Faked;
const ON: NotifyPrefs = { enabled: true, turnDone: true, approval: true, ask: true, feedbackReply: true };

let hidden = false;
let focused = true;
beforeEach(() => {
  vi.useFakeTimers();
  hidden = false;
  focused = true;
  vi.spyOn(document, "hasFocus").mockImplementation(() => focused);
  Object.defineProperty(document, "visibilityState", { configurable: true, get: () => (hidden ? "hidden" : "visible") });
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.useRealTimers();
});

function Probe({ port, machine = "", open = false }: { port: AgentPort; machine?: string; open?: boolean }) {
  const [n, setN] = useState(0);
  useFeedbackUnread(port, machine, n, setN, open);
  return <output>{n}</output>;
}
const tick = (ms: number) => act(async () => { await vi.advanceTimersByTimeAsync(ms); });
const setHidden = (v: boolean) => act(async () => { hidden = v; document.dispatchEvent(new Event("visibilitychange")); await vi.advanceTimersByTimeAsync(0); });
const focusEvent = () => act(async () => { window.dispatchEvent(new Event("focus")); await vi.advanceTimersByTimeAsync(0); });

describe("useFeedbackUnread polling", () => {
  it("never polls a person with no reports", async () => {
    const port = portOf(async () => mine(0, []));
    render(<Probe port={port} />);
    await tick(POLL_MS * 3);
    await focusEvent();
    await tick(FOCUS_GAP_MS * 2);
    await focusEvent();
    expect(port.myFeedback).toHaveBeenCalledTimes(1);
  });

  it("never polls when every report is settled", async () => {
    const port = portOf(async () => mine(0, ["fixed", "wontfix", "duplicate", "closed"]));
    render(<Probe port={port} />);
    await tick(POLL_MS * 3);
    expect(port.myFeedback).toHaveBeenCalledTimes(1);
  });

  it("shows a reply that arrives while the app stays open, on the next poll", async () => {
    let unread = 0;
    const port = portOf(async () => mine(unread, ["received"]));
    const { container } = render(<Probe port={port} />);
    await tick(0);
    expect(container.textContent).toBe("0");
    unread = 2;
    await tick(POLL_MS);
    expect(container.textContent).toBe("2");
    expect(port.myFeedback).toHaveBeenCalledTimes(2);
  });

  it("refreshes on focus at most once per 30 s", async () => {
    const port = portOf(async () => mine(0, ["received"]));
    render(<Probe port={port} />);
    await tick(0);
    await focusEvent();
    expect(port.myFeedback).toHaveBeenCalledTimes(1);
    await tick(FOCUS_GAP_MS);
    await focusEvent();
    expect(port.myFeedback).toHaveBeenCalledTimes(2);
  });

  it("pauses while hidden and refreshes when shown again", async () => {
    const port = portOf(async () => mine(0, ["received"]));
    render(<Probe port={port} />);
    await tick(0);
    await setHidden(true);
    await tick(POLL_MS * 3);
    expect(port.myFeedback).toHaveBeenCalledTimes(1);
    await setHidden(false);
    expect(port.myFeedback).toHaveBeenCalledTimes(2);
  });

  it("backs off on failure up to an hour, stays silent, and recovers", async () => {
    let fail = false;
    const port = portOf(async () => {
      if (fail) throw new Error("down");
      return mine(1, ["received"]);
    });
    const { container } = render(<Probe port={port} />);
    await tick(0);
    fail = true;
    await tick(POLL_MS);
    expect(port.myFeedback).toHaveBeenCalledTimes(2);
    await tick(POLL_MS);
    expect(port.myFeedback).toHaveBeenCalledTimes(2);
    await tick(POLL_MS);
    expect(port.myFeedback).toHaveBeenCalledTimes(3);
    await tick(POLL_CEILING_MS * 3);
    const after = port.myFeedback.mock.calls.length;
    await tick(POLL_CEILING_MS);
    expect(port.myFeedback.mock.calls.length).toBe(after + 1);
    expect(container.textContent).toBe("1");
    fail = false;
    await tick(POLL_CEILING_MS);
    expect(container.textContent).toBe("1");
    const recovered = port.myFeedback.mock.calls.length;
    await tick(POLL_MS);
    expect(port.myFeedback.mock.calls.length).toBe(recovered + 1);
  });

  it("refreshes once when the feedback panel closes, so a first report starts being watched", async () => {
    const port = portOf(async () => mine(0, []));
    const { rerender } = render(<Probe port={port} open />);
    await tick(0);
    rerender(<Probe port={port} open={false} />);
    await tick(0);
    expect(port.myFeedback).toHaveBeenCalledTimes(2);
  });
});

describe("useFeedbackUnread notice", () => {
  it("announces once when the count rises while the window is not focused", async () => {
    let unread = 1;
    const port = portOf(async () => mine(unread, ["received"]), ON);
    render(<Probe port={port} />);
    await tick(0);
    expect(port.announceFeedbackReply).not.toHaveBeenCalled();
    focused = false;
    await tick(POLL_MS);
    expect(port.announceFeedbackReply).not.toHaveBeenCalled();
    unread = 2;
    await tick(POLL_MS);
    expect(port.announceFeedbackReply).toHaveBeenCalledTimes(1);
    await tick(POLL_MS);
    expect(port.announceFeedbackReply).toHaveBeenCalledTimes(1);
  });

  it("stays quiet when the person is looking at the window", async () => {
    let unread = 0;
    const port = portOf(async () => mine(unread, ["received"]), ON);
    const { container } = render(<Probe port={port} />);
    await tick(0);
    unread = 3;
    await tick(POLL_MS);
    expect(container.textContent).toBe("3");
    expect(port.announceFeedbackReply).not.toHaveBeenCalled();
  });

  it("keeps polling a hidden window only for someone who asked to be notified", async () => {
    let unread = 0;
    const port = portOf(async () => mine(unread, ["received"]), ON);
    render(<Probe port={port} />);
    await tick(0);
    focused = false;
    await setHidden(true);
    unread = 1;
    await tick(POLL_MS);
    expect(port.announceFeedbackReply).toHaveBeenCalledTimes(1);
  });

  it("measures a rise from what the panel left on the rail, not from the last fetch", async () => {
    let unread = 3;
    const port = portOf(async () => mine(unread, ["received"]), ON);
    const { rerender } = render(<Probe port={port} open />);
    await tick(0);
    unread = 1;
    rerender(<Probe port={port} open={false} />);
    await tick(0);
    focused = false;
    unread = 2;
    await tick(POLL_MS);
    expect(port.announceFeedbackReply).toHaveBeenCalledTimes(1);
  });

  it("announces another maintainer reply on a report that was already unread", async () => {
    let replies = [{ id: 1, author: "maintainer", body: "", createdAt: "" }];
    const ask = async () => {
      const m = mine(1, ["needs_info"]);
      (m.items[0] as unknown as { replies: unknown[] }).replies = replies;
      return m;
    };
    const port = portOf(ask, ON);
    render(<Probe port={port} />);
    await tick(0);
    focused = false;
    await tick(POLL_MS);
    expect(port.announceFeedbackReply).not.toHaveBeenCalled();
    replies = [...replies, { id: 2, author: "maintainer", body: "", createdAt: "" }];
    await tick(POLL_MS);
    expect(port.announceFeedbackReply).toHaveBeenCalledTimes(1);
    await tick(POLL_MS);
    expect(port.announceFeedbackReply).toHaveBeenCalledTimes(1);
  });

  it("does not announce the reporter's own reply", async () => {
    let replies = [{ id: 1, author: "maintainer", body: "", createdAt: "" }];
    const ask = async () => {
      const m = mine(1, ["needs_info"]);
      (m.items[0] as unknown as { replies: unknown[] }).replies = replies;
      return m;
    };
    const port = portOf(ask, ON);
    render(<Probe port={port} />);
    await tick(0);
    focused = false;
    replies = [...replies, { id: 2, author: "user", body: "", createdAt: "" }];
    await tick(POLL_MS);
    expect(port.announceFeedbackReply).not.toHaveBeenCalled();
  });

  it("does not announce what the first fetch finds", async () => {
    focused = false;
    const port = portOf(async () => mine(4, ["received"]), ON);
    render(<Probe port={port} />);
    await tick(0);
    expect(port.announceFeedbackReply).not.toHaveBeenCalled();
  });
});

describe("useFeedbackUnread lifetime", () => {
  it("fires nothing after unmount", async () => {
    const port = portOf(async () => mine(0, ["received"]));
    const { unmount } = render(<Probe port={port} />);
    await tick(0);
    unmount();
    await tick(POLL_MS * 3);
    await focusEvent();
    expect(port.myFeedback).toHaveBeenCalledTimes(1);
  });

  it("drops an answer that lands after unmount", async () => {
    let land: (m: FeedbackMine) => void = () => {};
    const port = portOf(() => new Promise<FeedbackMine>((r) => { land = r; }), ON);
    const set = vi.fn();
    function Bare() {
      useFeedbackUnread(port, "", 0, set, false);
      return null;
    }
    const { unmount } = render(<Bare />);
    unmount();
    land(mine(5, ["received"]));
    await tick(POLL_MS);
    expect(set).not.toHaveBeenCalled();
    expect(port.myFeedback).toHaveBeenCalledTimes(1);
  });

  it("starts over when the machine changes: refetches, and its count is not a rise", async () => {
    focused = false;
    const first = portOf(async () => mine(0, ["received"]), ON);
    const second = portOf(async () => mine(7, ["received"]), ON);
    const { rerender, container } = render(<Probe port={first} machine="a" />);
    await tick(0);
    rerender(<Probe port={second} machine="b" />);
    await tick(0);
    expect(second.myFeedback).toHaveBeenCalledTimes(1);
    expect(container.textContent).toBe("7");
    expect(second.announceFeedbackReply).not.toHaveBeenCalled();
    await tick(POLL_MS);
    expect(first.myFeedback).toHaveBeenCalledTimes(1);
    expect(second.myFeedback).toHaveBeenCalledTimes(2);
  });

  it("keeps its schedule when only the pane behind the same machine changes", async () => {
    const first = portOf(async () => mine(0, ["received"]));
    const second = portOf(async () => mine(0, ["received"]));
    const { rerender } = render(<Probe port={first} machine="a" />);
    await tick(0);
    rerender(<Probe port={second} machine="a" />);
    await tick(0);
    expect(second.myFeedback).not.toHaveBeenCalled();
    await tick(POLL_MS);
    expect(second.myFeedback).toHaveBeenCalledTimes(1);
  });

  it("retries on focus when the first fetch failed", async () => {
    let fail = true;
    const port = portOf(async () => {
      if (fail) throw new Error("offline");
      return mine(2, ["received"]);
    });
    const { container } = render(<Probe port={port} />);
    await tick(0);
    fail = false;
    await tick(FOCUS_GAP_MS);
    await focusEvent();
    expect(container.textContent).toBe("2");
    await tick(POLL_MS);
    expect(port.myFeedback).toHaveBeenCalledTimes(3);
  });

  it("survives a StrictMode double mount with one request chain", async () => {
    const port = portOf(async () => mine(0, ["received"]));
    render(<StrictMode><Probe port={port} /></StrictMode>);
    await tick(0);
    const initial = port.myFeedback.mock.calls.length;
    expect(initial).toBeLessThanOrEqual(2);
    await tick(POLL_MS);
    expect(port.myFeedback.mock.calls.length).toBe(initial + 1);
  });
});
