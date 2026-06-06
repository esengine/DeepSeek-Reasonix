import { useEffect, useMemo, useRef, useState } from "react";

interface JumpBarProps {
  messages: { kind: string; text?: string; turn?: number }[];
  threadEl?: HTMLElement | null;
  onScrollToTurn?: (turn: number) => void;
}

export function JumpBar({ messages, threadEl, onScrollToTurn }: JumpBarProps) {
  const [hovered, setHovered] = useState<number | null>(null);
  const [active, setActive] = useState<number | null>(null);
  const barRef = useRef<HTMLDivElement>(null);
  const previewTop = useRef(0);
  const [showPreview, setShowPreview] = useState(false);

  const items = useMemo(
    () =>
      messages
        .filter((m): m is { kind: "user"; text: string; turn: number } =>
          m.kind === "user" && typeof m.text === "string" && typeof m.turn === "number",
        )
        .map((m) => ({ turn: m.turn, text: m.text.slice(0, 80) })),
    [messages],
  );

  useEffect(() => {
    if (items.length > 0) setActive(items[items.length - 1]!.turn);
  }, [items]);

  useEffect(() => {
    if (active === null) return;
    const el = barRef.current?.querySelector(`[data-turn="${active}"]`);
    el?.scrollIntoView({ block: "nearest" });
  }, [active]);

  if (items.length < 2) return null;

  const hoverIdx = hovered !== null ? items.findIndex((v) => v.turn === hovered) : -1;
  const hoverText = hovered !== null ? items.find((v) => v.turn === hovered)?.text : null;

  const onMove = (e: React.MouseEvent) => {
    const el = barRef.current;
    if (!el) return;
    const q = el.querySelectorAll<HTMLElement>(".jump-item");
    const barRect = el.getBoundingClientRect();
    let closest = -1;
    let closestDist = Infinity;
    q.forEach((item, i) => {
      const r = item.getBoundingClientRect();
      const midY = r.top + r.height / 2;
      const dist = Math.abs(e.clientY - midY);
      if (dist < closestDist) {
        closestDist = dist;
        closest = i;
        previewTop.current = midY - barRect.top;
      }
    });
    if (closest >= 0 && closest < items.length) {
      const turn = items[closest]?.turn;
      if (turn !== undefined) {
        setHovered(turn);
        setShowPreview(true);
      }
    }
  };

  const scrollTo = (turn: number) => {
    setActive(turn);
    if (onScrollToTurn) {
      onScrollToTurn(turn);
    } else {
      threadEl
        ?.querySelector(`[data-turn="${turn}"]`)
        ?.scrollIntoView({ behavior: "smooth", block: "start" });
    }
  };

  const dotProps = (
    idx: number,
    turn: number,
  ): { style: React.CSSProperties; "data-d"?: string } => {
    const isActive = active === turn;
    if (hoverIdx < 0) {
      return { style: { width: isActive ? 18 : 12, background: isActive ? "var(--accent)" : undefined } };
    }
    const d = Math.abs(idx - hoverIdx);
    const width = d === 0 ? 32 : d === 1 ? 20 : d === 2 ? 14 : isActive ? 18 : 12;
    const background = d <= 2 ? undefined : isActive ? "var(--accent)" : undefined;
    return {
      style: { width, transitionDelay: `${d * 20}ms`, background },
      "data-d": d <= 2 ? String(d) : undefined,
    };
  };

  return (
    <div className="jump-bar" ref={barRef} onMouseMove={onMove} onMouseLeave={() => { setHovered(null); setShowPreview(false); }}>
      <div className="jump-scroll">
        {items.map((item, idx) => (
          <div className="jump-item" key={item.turn} data-turn={item.turn} onClick={() => scrollTo(item.turn)}>
            <div className="jump-dot" {...dotProps(idx, item.turn)} />
          </div>
        ))}
      </div>
      {showPreview && hoverText && (
        <div className="jump-preview" style={{ top: previewTop.current }}>
          <span className="jump-text">{hoverText}</span>
        </div>
      )}
    </div>
  );
}
