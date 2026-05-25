import { useEffect, useMemo, useRef, useState } from "react";

interface JumpBarProps {
  messages: { kind: string; text?: string; turn?: number }[];
  threadEl: HTMLElement | null;
}

export function JumpBar({ messages, threadEl }: JumpBarProps) {
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

  // Scroll active bar into view when it changes
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
      if (turn !== undefined) { setHovered(turn); setShowPreview(true); }
    }
  };

  const scrollTo = (turn: number) => {
    setActive(turn);
    threadEl?.querySelector(`[data-turn="${turn}"]`)?.scrollIntoView({ behavior: "smooth", block: "start" });
  };

  return (
    <div className="jump-bar" ref={barRef} onMouseMove={onMove} onMouseLeave={() => { setHovered(null); setShowPreview(false); }}>
      <div className="jump-scroll">
        {items.map((item, idx) => {
          const dist = hoverIdx >= 0 ? Math.abs(idx - hoverIdx) : -1;
          const isActive = active === item.turn;
          const isHov = dist === 0;
          const isNear = dist === 1 || dist === 2;
          const w = isHov ? 32 : isNear ? (dist === 1 ? 20 : 14) : isActive ? 18 : 12;
          const bg = isHov || isNear ? undefined : isActive ? "var(--accent)" : undefined;
          return (
            <div className="jump-item" key={item.turn} data-turn={item.turn} onClick={() => scrollTo(item.turn)}>
              <div className="jump-dot" style={{ width: w, transitionDelay: `${dist * 20}ms`, background: bg }}
                data-d={dist >= 0 && dist <= 2 ? String(dist) : undefined} />
            </div>
          );
        })}
      </div>
      {showPreview && hoverText && (
        <div className="jump-preview" style={{ top: previewTop.current }}>
          <span className="jump-text">{hoverText}</span>
        </div>
      )}
    </div>
  );
}
