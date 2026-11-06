import { useEffect, useId, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import { Markdown } from "./Markdown";
import type { SideProgress } from "../port/side";
import { StudioIcon } from "./StudioIcon";
import { listenAction } from "./listen";
import { RunClock } from "./RunClock";

interface Turn {
  role: "me" | "side";
  text: string;
  quotes?: string[];
}

interface PendingTurn {
  question: string;
  quotes: string[];
  text: string;
  phase: SideProgress["kind"] | "failed";
  tool?: string;
  since: number;
}

const QUOTE_CAP = 20000;

// A question asked beside the session: read-only, kept only for as long as this
// panel is open, and never part of the main conversation. The kernel opens the
// conversation at the first question and drops it when the panel closes.
export function SidePanel({
  port,
  onClose,
  onQuote,
}: {
  port: AgentPort;
  onClose: () => void;
  onQuote?: (text: string) => void;
}) {
  const [turns, setTurns] = useState<Turn[]>([]);
  const [quotes, setQuotes] = useState<string[]>([]);
  const [draft, setDraft] = useState("");
  const [collapsed, setCollapsed] = useState(false);
  const contentId = useId();
  const [pending, setPending] = useState<PendingTurn | null>(null);
  const busy = pending !== null && pending.phase !== "failed";
  const [error, setError] = useState("");
  const panel = useRef<HTMLElement>(null);
  const opened = useRef<Promise<string> | null>(null);
  const id = useRef("");
  const live = useRef(true);

  useEffect(() => {
    live.current = true;
    return () => {
      live.current = false;
      const open = opened.current;
      opened.current = null;
      if (open) void open.then((held) => port.closeSide(held)).catch(() => {});
    };
  }, [port]);

  // Rectangles include interface zoom; the CSS height uses local pixels.
  // The pane bounds the room above the composer in either coordinate space.
  useEffect(() => {
    const el = panel.current;
    const host = el?.offsetParent;
    const pane = host?.closest(".pane");
    if (!el || !(host instanceof HTMLElement) || !pane) return;
    const fit = () => {
      const bounds = host.getBoundingClientRect();
      const scale = bounds.width / host.offsetWidth || 1;
      const room = (bounds.top - pane.getBoundingClientRect().top) / scale - 18;
      el.style.setProperty(
        "--side-room",
        `${Math.max(0, Math.min(520, Math.floor(room)))}px`,
      );
    };
    fit();
    const watch = new ResizeObserver(fit);
    watch.observe(host);
    watch.observe(pane);
    return () => watch.disconnect();
  }, []);

  // Pressing the button collapses the selection in some browsers, so the last
  // passage selected outside this panel is kept as it is made.
  const lastSelected = useRef("");
  useEffect(() => {
    const remember = () => {
      const sel = window.getSelection();
      const text = sel?.toString().trim() ?? "";
      if (text && !(sel?.anchorNode && panel.current?.contains(sel.anchorNode)))
        lastSelected.current = text;
    };
    return listenAction(document, "selectionchange", {
      action: "side.quote",
      listener: remember,
    });
  }, []);

  const quoteSelection = () => {
    const sel = window.getSelection();
    const live = sel?.toString().trim() ?? "";
    const text =
      live && !(sel?.anchorNode && panel.current?.contains(sel.anchorNode))
        ? live
        : lastSelected.current;
    if (!text) {
      setError(t("composer.select_some_text_in_the"));
      return;
    }
    lastSelected.current = "";
    setError("");
    setQuotes((all) =>
      all.includes(text) ? all : [...all, text.slice(0, QUOTE_CAP)],
    );
  };

  const send = async () => {
    const question = draft.trim();
    if (!question || busy) return;
    const sent = quotes;
    setPending({
      question,
      quotes: sent,
      text: "",
      phase: "waiting",
      since: Date.now(),
    });
    setError("");
    // What was asked leaves the box the moment it is sent, and comes back only if the ask fails.
    setDraft("");
    setQuotes([]);
    try {
      opened.current ??= port.openSide().then((held) => {
        id.current = held;
        return held;
      });
      const held = await opened.current;
      if (!live.current) return;
      const answer = await port.askSide(held, question, sent, (progress) => {
        if (!live.current) return;
        setPending(
          (current) =>
            current && {
              ...current,
              text:
                progress.kind === "text"
                  ? current.text + (progress.text ?? "")
                  : current.text,
              phase: progress.kind,
              tool: progress.tool,
            },
        );
      });
      if (!live.current) return;
      setTurns((all) => [
        ...all,
        { role: "me", text: question, quotes: sent },
        { role: "side", text: answer },
      ]);
      setPending(null);
    } catch (e) {
      if (!live.current) return;
      setDraft((now) => now || question);
      setQuotes((now) => (now.length ? now : sent));
      opened.current = id.current ? opened.current : null;
      setError(reason(e));
      setPending((current) => current && { ...current, phase: "failed" });
    }
  };

  const close = () => {
    live.current = false;
    const open = opened.current;
    opened.current = null;
    if (open) void open.then((held) => port.closeSide(held)).catch(() => {});
    onClose();
  };

  return (
    <aside
      className="sidepanel"
      data-collapsed={collapsed || undefined}
      ref={panel}
      role="complementary"
      aria-label={t("composer.side_question")}
      data-action-keydown="side.close"
      onKeyDown={(e) => {
        if (e.key !== "Escape" || e.nativeEvent.isComposing) return;
        e.stopPropagation();
        close();
      }}
    >
      <header>
        <StudioIcon name="chat" />
        <b>{t("composer.side_question")}</b>
        {(
          [
            "composer.side_badge_read_only",
            "composer.side_badge_not_main",
            "composer.side_badge_gone",
          ] as const
        ).map((key) => (
          <span className="sidebadge" key={key}>
            {t(key)}
          </span>
        ))}
        <div className="sideactions">
          <button
            type="button"
            className="sidecollapse"
            data-action="side.collapse"
            aria-expanded={!collapsed}
            aria-controls={contentId}
            aria-label={t(
              collapsed
                ? "composer.expand_the_side_question"
                : "composer.collapse_the_side_question",
            )}
            title={t(
              collapsed
                ? "composer.expand_the_side_question"
                : "composer.collapse_the_side_question",
            )}
            onClick={() => setCollapsed((held) => !held)}
          >
            <StudioIcon name="down" />
          </button>
          <button
            type="button"
            data-action="side.close"
            aria-label={t("composer.close_the_side_question")}
            title={t("composer.close_the_side_question")}
            onClick={close}
          >
            <StudioIcon name="close" />
          </button>
        </div>
      </header>
      <div className="sidecontent" id={contentId} hidden={collapsed}>
        <div className="sidebody" aria-live="polite">
          {turns.length === 0 && !pending && (
            <p className="sidehint sideempty">
              {t("composer.ask_about_this_code_or")}
            </p>
          )}
          {turns.map((turn, i) => (
            <div key={i} className="sideturn" data-role={turn.role}>
              {turn.quotes?.map((q) => (
                <blockquote key={q}>{q}</blockquote>
              ))}
              {turn.role === "side" ? (
                <Markdown text={turn.text} images={false} />
              ) : (
                <p>{turn.text}</p>
              )}
              {turn.role === "side" && onQuote && (
                <button
                  type="button"
                  className="sidetomain"
                  data-action="side.quote-answer"
                  onClick={() =>
                    onQuote(
                      `${turn.text
                        .split("\n")
                        .map((line) => `> ${line}`)
                        .join("\n")}\n`,
                    )
                  }
                >
                  <StudioIcon name="quote" />
                  {t("composer.side_quote_to_main")}
                </button>
              )}
            </div>
          ))}
          {pending && (
            <>
              <div className="sideturn" data-role="me">
                {pending.quotes.map((quote) => (
                  <blockquote key={quote}>{quote}</blockquote>
                ))}
                <p>{pending.question}</p>
              </div>
              {pending.text && (
                <div className="sideturn" data-role="side">
                  <Markdown text={pending.text} images={false} />
                  {pending.phase === "failed" && (
                    <small className="sidehint">
                      {t("common.interrupted")}
                    </small>
                  )}
                </div>
              )}
            </>
          )}
          {error && (
            <p className="sideerr" role="alert">
              {error}
            </p>
          )}
          {quotes.length > 0 && !busy && (
            <ul className="sidequotes">
              {quotes.map((q, i) => (
                <li key={q}>
                  <em>{t("composer.side_quote_label")}</em>
                  <span>{q}</span>
                  <button
                    type="button"
                    data-action="side.unquote"
                    aria-label={t("composer.remove_quote_n", { n: i + 1 })}
                    onClick={() =>
                      setQuotes((all) => all.filter((x) => x !== q))
                    }
                  >
                    <StudioIcon name="close" />
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
        {busy && pending && (
          <p className="sidehint sideprogress" role="status">
            {pending.phase === "text"
              ? t("composer.answering")
              : pending.phase === "thinking"
                ? t("composer.thinking")
                : pending.phase === "tool"
                  ? t("common.working")
                  : t("composer.waiting_for_the_model")}
            {pending.tool && <> · {pending.tool}</>}
            {" · "}
            <RunClock since={pending.since} />
          </p>
        )}
        <footer>
          {quotes.length === 0 && (
            <p className="sidenote">{t("composer.side_select_first")}</p>
          )}
          <button
            type="button"
            className="sidequote"
            data-action="side.quote"
            disabled={busy}
            onClick={quoteSelection}
          >
            {t("common.quote_selected_text")}
          </button>
          <textarea
            aria-label={t("composer.ask_the_side_question")}
            value={draft}
            rows={1}
            readOnly={busy}
            data-action-change="side.draft"
            data-action-keydown="side.send"
            placeholder={t("composer.ask")}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (
                e.key === "Enter" &&
                !e.shiftKey &&
                !e.nativeEvent.isComposing
              ) {
                e.preventDefault();
                void send();
              }
            }}
          />
          <button
            type="button"
            className="sidesend"
            data-action="side.send"
            disabled={busy || !draft.trim()}
            onClick={() => void send()}
          >
            {t("common.send")}
          </button>
        </footer>
      </div>
    </aside>
  );
}
