import { useEffect, useRef } from "react";
import type { FeedbackItem, FeedbackMine, FeedbackStatus } from "../port/feedback";
import type { AgentPort } from "../port/port";

export const POLL_MS = 10 * 60_000;
export const POLL_CEILING_MS = 60 * 60_000;
export const FOCUS_GAP_MS = 30_000;

const SETTLED: readonly FeedbackStatus[] = ["fixed", "wontfix", "duplicate", "closed"];

const latestReply = (i: FeedbackItem) => i.replies?.reduce((id, r) => (r.author === "maintainer" && r.id > id ? r.id : id), 0) ?? 0;

const hasOpenItem = (m: FeedbackMine) => m.items.some((i) => !SETTLED.includes(i.status));

/** Keeps the rail's unread count honest while the app stays open. Only a person
 *  with an unsettled report is ever polled, a failed background read is silent,
 *  and a hidden window keeps polling only for someone who asked to be notified.
 *  `machine` names whose kernel answers: a change of it starts over. */
export function useFeedbackUnread(
  port: AgentPort | null,
  machine: string,
  unread: number,
  setUnread: (n: number) => void,
  panelOpen: boolean,
) {
  const portRef = useRef(port);
  portRef.current = port;
  const ready = port !== null;
  const unreadRef = useRef(unread);
  unreadRef.current = unread;
  const setRef = useRef(setUnread);
  setRef.current = setUnread;
  const refreshRef = useRef<(() => void) | null>(null);

  useEffect(() => {
    if (!ready) return;
    let alive = true;
    let timer: number | undefined;
    let epoch = 0;
    let open = false;
    let seeded = false;
    const answered = new Map<string, number>();
    let failures = 0;
    let last = 0;
    let inflight = false;

    const visible = () => document.visibilityState !== "hidden";
    const wantsNotice = () =>
      Promise.resolve()
        .then(() => portRef.current?.notifyPrefs())
        .then((p) => !!p && p.enabled && p.feedbackReply, () => false);
    const arm = () => {
      timer = window.setTimeout(refresh, Math.min(POLL_MS * 2 ** failures, POLL_CEILING_MS));
    };
    const schedule = () => {
      window.clearTimeout(timer);
      const mine = ++epoch;
      if (!alive || !open) return;
      if (visible()) return arm();
      void wantsNotice().then((ok) => {
        if (ok && alive && mine === epoch && !visible()) arm();
      });
    };
    function refresh() {
      const p = portRef.current;
      if (!p || inflight) return;
      inflight = true;
      last = Date.now();
      p.myFeedback().then(
        (m) => {
          failures = 0;
          open = hasOpenItem(m);
          if (!alive) return;
          let replied = false;
          for (const item of m.items) {
            const id = latestReply(item);
            if (id > (answered.get(item.receipt) ?? 0)) replied = true;
            answered.set(item.receipt, id);
          }
          const rose = seeded && (m.unread > unreadRef.current || replied);
          seeded = true;
          setRef.current(m.unread);
          if (rose && !document.hasFocus()) void Promise.resolve().then(() => p.announceFeedbackReply()).catch(() => {});
        },
        () => {
          failures += 1;
        },
      ).finally(() => {
        inflight = false;
        schedule();
      });
    }
    const wake = () => {
      if (visible() && (open || !seeded) && Date.now() - last >= FOCUS_GAP_MS) refresh();
      else schedule();
    };
    refreshRef.current = refresh;
    refresh();
    document.addEventListener("visibilitychange", wake);
    window.addEventListener("focus", wake);
    return () => {
      alive = false;
      refreshRef.current = null;
      window.clearTimeout(timer);
      document.removeEventListener("visibilitychange", wake);
      window.removeEventListener("focus", wake);
    };
  }, [ready, machine]);

  const was = useRef(panelOpen);
  useEffect(() => {
    if (was.current && !panelOpen) refreshRef.current?.();
    was.current = panelOpen;
  }, [panelOpen]);
}
