import { useEffect, useRef, useState } from "react";
import { I } from "../icons";

export function fmtElapsed(ms: number): string {
  const s = ms / 1000;
  return s < 10 ? `${s.toFixed(1)}s` : `${Math.floor(s)}s`;
}

export function useElapsed(active: boolean, startAt?: number): number {
  const [ms, setMs] = useState(0);
  const start = useRef<number | null>(null);
  useEffect(() => {
    if (!active) {
      setMs(0);
      start.current = null;
      return;
    }
    start.current = startAt ?? performance.now();
    const id = setInterval(() => {
      if (start.current !== null) setMs(performance.now() - start.current);
    }, 80);
    return () => clearInterval(id);
  }, [active, startAt]);
  return ms;
}

export function ThinkingPill({
  phase = "thinking",
  label,
  elapsedMs,
}: {
  phase?: "queued" | "thinking" | "tool";
  label: string;
  elapsedMs: number;
}) {
  const color =
    phase === "queued"
      ? "var(--muted)"
      : phase === "tool"
        ? "var(--warning)"
        : "var(--accent)";
  return (
    <div className="thinking">
      <span className="dots" style={{ color }}>
        <span style={{ background: color }} />
        <span style={{ background: color }} />
        <span style={{ background: color }} />
      </span>
      <span className="label">
        <span className="sh">{label}</span>
      </span>
      <span className="timer">{fmtElapsed(elapsedMs)}</span>
    </div>
  );
}

export function LiveReasoning({ lines }: { lines: string[] }) {
  return (
    <div className="live-reason">
      <div className="head">
        <span className="dot" /> live · reasoning
      </div>
      {lines.map((line, i) => (
        <div key={i}>
          {line}
          {i === lines.length - 1 ? <span className="stream-caret" /> : null}
        </div>
      ))}
    </div>
  );
}

export function ToolRunningCard({
  kind = "tool",
  name,
  elapsedMs,
  logLines,
}: {
  kind?: "shell" | "fetch" | "search" | "tool";
  name: string;
  elapsedMs: number;
  logLines?: { text: string; tone?: "ok" | "dim" }[];
}) {
  const ic =
    kind === "shell" ? <I.terminal size={12} /> : kind === "fetch" ? <I.globe size={12} /> : kind === "search" ? <I.search size={12} /> : <I.wrench size={12} />;
  return (
    <div className="skel-card">
      <div className="h">
        <span className="ico">{ic}</span>
        <span className="kind">{kind}</span>
        <span style={{ color: "var(--fg)", fontWeight: 500 }}>{name}</span>
        <span className="grow" />
        <span className="pill-tag run">running</span>
        <span className="timer">{fmtElapsed(elapsedMs)}</span>
      </div>
      {logLines && logLines.length > 0 ? (
        <div className="live-log">
          {logLines.map((ln, i) => (
            <div
              key={i}
              className={`line ${ln.tone ?? ""}`}
              style={{ animationDelay: `${i * 0.25}s` }}
            >
              {ln.text}
            </div>
          ))}
        </div>
      ) : (
        <div className="body">
          <div className="skel-line w-90" />
          <div className="skel-line w-70" />
          <div className="skel-line w-60" />
        </div>
      )}
    </div>
  );
}

export function PendingUserMsg({ text, elapsedMs }: { text: string; elapsedMs: number }) {
  return (
    <div className="msg user">
      <div className="avatar">YOU</div>
      <div className="body">
        <div className="who">
          <span className="name">你</span>
          <span className="time">{(elapsedMs / 1000).toFixed(1)}s ago</span>
        </div>
        <div className="msg-text user-pending">{text}</div>
        <div className="user-status">
          <span className="spin" />
          <span>已送达 · 等待 agent 响应</span>
        </div>
      </div>
    </div>
  );
}
