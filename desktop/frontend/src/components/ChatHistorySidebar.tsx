import { useCallback, useEffect, useRef, useState, type MouseEvent as ReactMouseEvent } from "react";
import { useT } from "../lib/i18n";
import type { QuestionAnchor } from "../lib/transcriptGrouping";

interface ChatHistorySidebarProps {
  questions: QuestionAnchor[];
  onJump: (question: QuestionAnchor) => void;
}

/**
 * ChatGPT-style conversation history sidebar.
 *
 * A thin vertical strip with horizontal bars (dots) on the right edge of the
 * transcript. Hovering reveals a popover listing ALL user messages — click any
 * to jump to that position. The popover stays open while the cursor is over
 * it, and auto-closes on mouseleave.
 */
export function ChatHistorySidebar({ questions, onJump }: ChatHistorySidebarProps) {
  const t = useT();
  const [active, setActive] = useState<number | null>(null);
  const [panelOpen, setPanelOpen] = useState(false);
  const barRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const closeTimer = useRef<ReturnType<typeof setTimeout>>(null);
  const questionsRef = useRef(questions);
  questionsRef.current = questions;

  // Track last active turn to avoid redundant setActive
  const lastActiveRef = useRef<number | null>(null);
  // When panel is open, pause all scroll-sync — only clicks change active
  const pauseSyncRef = useRef(false);

  // Sync active dot: map transcript scroll proportion to question index
  useEffect(() => {
    const transcript = document.querySelector(".transcript") as HTMLElement | null;
    if (!transcript) return;

    const sync = () => {
      if (pauseSyncRef.current) return; // panel open → no auto-sync

      const cqs = questionsRef.current;
      if (cqs.length === 0) return;

      const { scrollTop, scrollHeight, clientHeight } = transcript;
      const ratio = scrollHeight > clientHeight
        ? Math.max(0, Math.min(1, scrollTop / (scrollHeight - clientHeight)))
        : 0;

      const idx = Math.round(ratio * (cqs.length - 1));
      const nextActive = cqs[idx]?.turn ?? null;

      if (nextActive != null && nextActive !== lastActiveRef.current) {
        lastActiveRef.current = nextActive;
        setActive(nextActive);
      }
    };

    sync();
    transcript.addEventListener("scroll", sync, { passive: true });
    const ro = new ResizeObserver(() => sync());
    ro.observe(transcript);
    return () => {
      transcript.removeEventListener("scroll", sync);
      ro.disconnect();
    };
  }, []);

  // When panel opens, scroll to the active row — but DON'T auto-scroll on active changes
  useEffect(() => {
    if (!panelOpen || active === null) return;
    const list = panelRef.current?.querySelector(".ch-panel__list");
    if (!list) return;
    const activeRow = list.querySelector(".ch-row--active");
    if (activeRow) {
      activeRow.scrollIntoView({ block: "center" });
    } else {
      list.scrollTop = list.scrollHeight;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [panelOpen]);

  const closestQuestionFromY = (clientY: number): { question: QuestionAnchor; idx: number } | null => {
    const el = barRef.current;
    if (!el) return null;
    const markers = el.querySelectorAll<HTMLElement>(".ch-item");
    let closest = -1;
    let closestDist = Infinity;
    markers.forEach((item, index) => {
      const rect = item.getBoundingClientRect();
      const dist = Math.abs(clientY - (rect.top + rect.height / 2));
      if (dist < closestDist) { closestDist = dist; closest = index; }
    });
    if (closest < 0) return null;
    return { question: questions[closest], idx: closest };
  };

  const onBarMove = useCallback((e: ReactMouseEvent<HTMLDivElement>) => {
    const closest = closestQuestionFromY(e.clientY);
    if (!closest) return;
    pauseSyncRef.current = true;
    setPanelOpen(true);
    if (closeTimer.current) { clearTimeout(closeTimer.current); closeTimer.current = null; }
  }, [questions]);

  const closePanel = useCallback(() => {
    pauseSyncRef.current = false;
    setPanelOpen(false);
  }, []);

  const onBarLeave = useCallback(() => {
    closeTimer.current = setTimeout(closePanel, 150);
  }, [closePanel]);

  const onPanelEnter = useCallback(() => {
    if (closeTimer.current) { clearTimeout(closeTimer.current); closeTimer.current = null; }
  }, []);

  const onPanelLeave = useCallback(() => {
    closeTimer.current = setTimeout(closePanel, 300);
  }, [closePanel]);

  const scrollTo = useCallback((q: QuestionAnchor) => {
    setActive(q.turn);
    onJump(q);
  }, [onJump]);

  const onBarMouseDown = useCallback((e: ReactMouseEvent<HTMLDivElement>) => {
    const closest = closestQuestionFromY(e.clientY);
    if (!closest) return;
    e.preventDefault();
    scrollTo(closest.question);
  }, [scrollTo]);

  if (questions.length === 0) return null;

  return (
    <nav
      className="ch-sidebar"
      ref={barRef}
      aria-label={t("questionNav.label")}
      onMouseMove={onBarMove}
      onMouseLeave={onBarLeave}
    >
      <div className="ch-sidebar__rail" onMouseDown={onBarMouseDown}>
        {questions.map((q) => (
          <button
            className="ch-item"
            key={q.id}
            type="button"
            data-turn={q.turn}
            aria-label={t("questionNav.jump", { n: q.turn + 1 })}
            tabIndex={0}
            onClick={() => { scrollTo(q); setPanelOpen(true); pauseSyncRef.current = true; }}
          >
            <span className="ch-dot" data-active={active === q.turn ? "true" : undefined} />
          </button>
        ))}
      </div>

      {panelOpen && (
        <div
          className="ch-panel"
          ref={panelRef}
          onMouseEnter={onPanelEnter}
          onMouseLeave={onPanelLeave}
        >
          <div className="ch-panel__list">
            {questions.map((q, idx) => {
              const isCurrent = active === q.turn;
              return (
                <button
                  className={`ch-row${isCurrent ? " ch-row--active" : ""}`}
                  key={q.id}
                  type="button"
                  onClick={() => scrollTo(q)}
                >
                  <span className="ch-row__num">{idx + 1}</span>
                  <span className="ch-row__text">{q.text}</span>
                </button>
              );
            })}
          </div>
        </div>
      )}
    </nav>
  );
}
