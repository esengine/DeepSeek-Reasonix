import { useCallback, useEffect, useRef, useState } from "react";
import { t } from "../i18n";

// Re-sending one prompt on a timer, inside the conversation it belongs to.
//
// This is not the scheduler. A schedule starts a supervised child in a fresh
// read-only session and reports a result back; a loop keeps talking in the
// session already on screen, which is what a person wants when they are
// watching the answer change. Both exist because neither answers the other's
// question.

/** The floor and the ceiling come from the same reasoning as v1: below five
 *  seconds a loop queues turns faster than they finish, and past six hours the
 *  gap is long enough that it should survive a restart, which a timer in a
 *  window does not. */
export const MIN_LOOP_INTERVAL_MS = 5_000;
export const MAX_LOOP_INTERVAL_MS = 6 * 60 * 60_000;

/** How long to wait before trying a skipped firing again. A turn already
 *  running is the one thing that makes a loop wait rather than stack. */
const BUSY_RETRY_MS = 1_000;

export function parseLoopInterval(raw: string): number | null {
  const text = raw.trim().toLowerCase();
  const m = /^([0-9]+(?:\.[0-9]+)?)(s|sec|secs|m|min|mins|h|hr|hrs)?$/.exec(text);
  if (!m) return null;
  const n = Number.parseFloat(m[1] ?? "");
  if (!Number.isFinite(n) || n <= 0) return null;
  const unit = m[2] ?? "s";
  const ms = unit.startsWith("s")
    ? Math.round(n * 1000)
    : unit.startsWith("m")
      ? Math.round(n * 60_000)
      : Math.round(n * 3_600_000);
  if (ms < MIN_LOOP_INTERVAL_MS || ms > MAX_LOOP_INTERVAL_MS) return null;
  return ms;
}

export type LoopCommand =
  | { kind: "start"; intervalMs: number; prompt: string }
  | { kind: "stop" }
  | { kind: "status" }
  | { kind: "error"; reason: "usage" | "no-prompt" };

/** What a line typed at the composer means, or null when it is not a loop line
 *  at all — the common case, and the one that has to stay cheap. */
export function parseLoopCommand(text: string): LoopCommand | null {
  const trimmed = text.trim();
  if (!/^\/loop(\s|$)/i.test(trimmed)) return null;
  const rest = trimmed.slice(5).trim();
  if (rest === "") return { kind: "status" };
  const [head, ...tail] = rest.split(/\s+/);
  if (/^(stop|off|cancel)$/i.test(head ?? "")) return { kind: "stop" };
  const intervalMs = parseLoopInterval(head ?? "");
  if (intervalMs === null) return { kind: "error", reason: "usage" };
  const prompt = tail.join(" ").trim();
  if (prompt === "") return { kind: "error", reason: "no-prompt" };
  return { kind: "start", intervalMs, prompt };
}

export interface ActiveLoop {
  prompt: string;
  intervalMs: number;
  /** Unix ms at which the next firing is due. */
  dueAt: number;
  iter: number;
}

/** Owns the one active loop of a conversation and its timer.
 *
 *  `fire` is the composer's own send, so a loop turn is an ordinary turn: it
 *  appears in the transcript, it can be stopped, and it costs what a typed
 *  message costs. `busy` is read at each firing rather than captured, because a
 *  turn can start after the timer was armed. */
export function useLoopMode({ fire, busy }: { fire: (text: string) => Promise<unknown>; busy: boolean }) {
  const [loop, setLoop] = useState<ActiveLoop | null>(null);
  const loopRef = useRef<ActiveLoop | null>(null);
  const busyRef = useRef(busy);
  const fireRef = useRef(fire);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Written from an effect, not during render: a render React throws away would
  // otherwise leave the ref naming a loop that was never committed.
  useEffect(() => {
    loopRef.current = loop;
  }, [loop]);
  useEffect(() => {
    busyRef.current = busy;
  }, [busy]);
  useEffect(() => {
    fireRef.current = fire;
  }, [fire]);

  const stop = useCallback(() => {
    if (timer.current) {
      clearTimeout(timer.current);
      timer.current = null;
    }
    setLoop(null);
  }, []);

  const start = useCallback((intervalMs: number, prompt: string) => {
    if (timer.current) {
      clearTimeout(timer.current);
      timer.current = null;
    }
    setLoop({ prompt, intervalMs, dueAt: Date.now() + intervalMs, iter: 0 });
  }, []);

  useEffect(() => {
    if (!loop) return;
    const delay = Math.max(0, loop.dueAt - Date.now());
    const handle = setTimeout(() => {
      timer.current = null;
      // A turn already running means this firing is skipped, not queued: a loop
      // that stacked turns on a slow answer would end every gap with two.
      if (busyRef.current) {
        setLoop((cur) => (cur ? { ...cur, dueAt: Date.now() + BUSY_RETRY_MS } : cur));
        return;
      }
      const cur = loopRef.current;
      if (!cur) return;
      // The next firing is scheduled from now, not from the end of this turn,
      // so a slow answer does not stretch the cadence it asked for.
      setLoop((c) => (c ? { ...c, iter: c.iter + 1, dueAt: Date.now() + c.intervalMs } : c));
      void fireRef.current(cur.prompt).catch(() => {
        // A submit the kernel refuses would fail identically next time; stop
        // rather than repeat the refusal on the screen every interval.
        setLoop(null);
      });
    }, delay);
    timer.current = handle;
    return () => clearTimeout(handle);
  }, [loop]);

  useEffect(() => stop, [stop]);

  /** Handles a composer line that is a loop command. Returns true when it was
   *  one, so the caller sends nothing else down the ordinary path. */
  const command = useCallback(
    (text: string): { handled: true; reply: string } | { handled: false } => {
      const parsed = parseLoopCommand(text);
      if (!parsed) return { handled: false };
      switch (parsed.kind) {
        case "start":
          start(parsed.intervalMs, parsed.prompt);
          return { handled: true, reply: "" };
        case "stop": {
          const was = loopRef.current;
          stop();
          return { handled: true, reply: was ? t("已停止循环（共 {n} 次）", { n: was.iter }) : t("当前没有在跑的循环") };
        }
        case "status": {
          const cur = loopRef.current;
          return { handled: true, reply: cur ? loopStatusLine(cur) : t("当前没有在跑的循环") };
        }
        default:
          return {
            handled: true,
            reply: parsed.reason === "no-prompt"
              ? t("用法：/loop 30s 你的提示词 —— 间隔对了，但没写要发什么")
              : t("用法：/loop <间隔> <提示词>（5s–6h），/loop stop 停止，/loop 查看状态"),
          };
      }
    },
    [start, stop],
  );

  return { loop, command, stop };
}

/** What `/loop` alone reports: the prompt in flight, when it fires next, and how
 *  many times it has fired. Kept short because it is one line above a box the
 *  person is typing into. */
export function loopStatusLine(loop: ActiveLoop): string {
  const wait = Math.max(0, loop.dueAt - Date.now());
  const secs = Math.round(wait / 1000);
  const when = wait <= 0 ? "0s" : secs < 60 ? `${secs}s` : `${Math.floor(secs / 60)}m${secs % 60 ? `${secs % 60}s` : ""}`;
  return `${loop.prompt.slice(0, 36)} · ${when} · #${loop.iter}`;
}

/** The standing line above the composer while a loop runs. No countdown: that
 *  would need a tick every second to stay true, and a line that is one second
 *  stale reads as broken. `/loop` answers the exact time when asked. */
export function loopLabel(loop: ActiveLoop): string {
  const every = loop.intervalMs % 60_000 === 0 ? `${loop.intervalMs / 60_000}m` : `${Math.round(loop.intervalMs / 1000)}s`;
  return t("循环中 · 每 {every} · 已 {n} 次 · {prompt}", {
    every,
    n: loop.iter,
    prompt: loop.prompt.length > 36 ? `${loop.prompt.slice(0, 33)}…` : loop.prompt,
  });
}
