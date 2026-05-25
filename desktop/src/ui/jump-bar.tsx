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

  const barProps = (idx: number): { style: React.CSSProperties; "data-d"?: string } => {
    if (hoverIdx < 0) return { style: { width: 12 } };
    const d = Math.abs(idx - hoverIdx);
    const width = d === 0 ? 32 : d === 1 ? 20 : d === 2 ? 14 : 12;
    return { style: { width, transitionDelay: `${d * 20}ms` }, "data-d": d <= 2 ? String(d) : undefined };
  };

  return (
    <div className="jump-bar" ref={barRef} onMouseMove={onMove} onMouseLeave={() => { setHovered(null); setShowPreview(false); }}>
      <div className="jump-scroll">
        {visible.map((item, idx) => (
        <div className="jump-item" key={item.turn} onClick={() => scrollTo(item.turn)}>
          <div className="jump-dot" {...barProps(idx)} />
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
