import { useMemo, useRef, useState } from "react";

interface JumpBarProps {
  messages: { kind: string; text?: string; turn?: number }[];
  threadEl: HTMLElement | null;
}

export function JumpBar({ messages, threadEl }: JumpBarProps) {
  const [hovered, setHovered] = useState<number | null>(null);
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

  if (items.length < 2) return null;

  const visible = items;
  const hoverIdx = hovered !== null ? visible.findIndex((v) => v.turn === hovered) : -1;
  const hoverText = hovered !== null ? visible.find((v) => v.turn === hovered)?.text : null;

  const onMove = (e: React.MouseEvent) => {
    const el = barRef.current;
    if (!el) return;
    const items = el.querySelectorAll<HTMLElement>(".jump-item");
    const barRect = el.getBoundingClientRect();
    let closest = -1;
    let closestDist = Infinity;
    items.forEach((item, i) => {
      const r = item.getBoundingClientRect();
      const midY = r.top + r.height / 2;
      const dist = Math.abs(e.clientY - midY);
      if (dist < closestDist) {
        closestDist = dist;
        closest = i;
        previewTop.current = midY - barRect.top;
      }
    });
    if (closest >= 0 && closest < visible.length) {
      const turn = visible[closest]?.turn;
      if (turn !== undefined) {
        setHovered(turn);
        setShowPreview(true);
      }
    }
  };

  const scrollTo = (turn: number) => {
    const el = threadEl?.querySelector(`[data-turn="${turn}"]`);
    el?.scrollIntoView({ behavior: "smooth", block: "start" });
  };

  const w = (idx: number): number => {
    if (hoverIdx < 0) return 12;
    const d = Math.abs(idx - hoverIdx);
    if (d === 0) return 32;
    if (d === 1) return 20;
    if (d === 2) return 14;
    return 12;
  };

  const delay = (idx: number): string =>
    hoverIdx < 0 ? "0ms" : `${Math.abs(idx - hoverIdx) * 20}ms`;

  return (
    <div className="jump-bar" ref={barRef} onMouseMove={onMove} onMouseLeave={() => { setHovered(null); setShowPreview(false); }}>
      <div className="jump-scroll">
        {visible.map((item, idx) => (
        <div className="jump-item" key={item.turn} onClick={() => scrollTo(item.turn)}>
          <div
            className="jump-dot"
            style={{ width: w(idx), transitionDelay: delay(idx) }}
            data-active={hoverIdx >= 0 && Math.abs(idx - hoverIdx) <= 2 || undefined}
          />
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
