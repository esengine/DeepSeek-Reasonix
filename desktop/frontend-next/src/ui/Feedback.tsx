import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import { useEscape } from "./dismiss";
import { listenAction } from "./listen";
import { FeedbackForm } from "./FeedbackForm";
import { FeedbackMine } from "./FeedbackMine";
import { StudioIcon } from "./StudioIcon";

export type FeedbackTab = "send" | "mine";

const TABS: readonly FeedbackTab[] = ["send", "mine"];
const TAB_LABEL: Record<FeedbackTab, string> = { send: "发送反馈", mine: "我的反馈" };
const FOCUSABLE = 'a[href], button:not(:disabled), input:not(:disabled), textarea:not(:disabled), select:not(:disabled), [tabindex]:not([tabindex="-1"])';

interface Props {
  port: AgentPort;
  tab?: FeedbackTab;
  onClose: () => void;
  onError: (message: string) => void;
  onUnread?: (n: number) => void;
}

export function Feedback({ port, tab: first = "send", onClose, onError, onUnread }: Props) {
  const [tab, setTab] = useState<FeedbackTab>(first);
  const card = useRef<HTMLDivElement>(null);
  const opener = useRef<Element | null>(document.activeElement);
  useEscape(true, onClose);

  useEffect(() => {
    const back = opener.current;
    card.current?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]')?.focus();
    return () => {
      if (back instanceof HTMLElement) back.focus();
    };
  }, []);

  useEffect(() => {
    const el = card.current;
    if (!el) return;
    const trap = (e: Event) => {
      const ev = e as globalThis.KeyboardEvent;
      if (ev.key !== "Tab") return;
      const all = [...el.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((n) => n.offsetParent !== null || n === document.activeElement);
      const head = all[0];
      const tail = all[all.length - 1];
      if (!head || !tail) return;
      const inside = el.contains(document.activeElement);
      if (ev.shiftKey && (document.activeElement === head || !inside)) {
        ev.preventDefault();
        tail.focus();
      } else if (!ev.shiftKey && (document.activeElement === tail || !inside)) {
        ev.preventDefault();
        head.focus();
      }
    };
    return listenAction(el, "keydown", { action: "feedback.focus", listener: trap });
  }, []);

  const openLink = (url: string) => void port.openExternal(url).catch((e) => onError(reason(e)));

  const walk = (e: KeyboardEvent, at: FeedbackTab) => {
    const step = e.key === "ArrowRight" ? 1 : e.key === "ArrowLeft" ? -1 : 0;
    if (!step) return;
    e.preventDefault();
    const next = TABS[(TABS.indexOf(at) + step + TABS.length) % TABS.length]!;
    setTab(next);
    requestAnimationFrame(() => card.current?.querySelector<HTMLElement>(`#fbk-tab-${next}`)?.focus());
  };

  return (
    <div className="fbk-veil">
      <div className="fbk" ref={card} role="dialog" aria-modal="true" aria-labelledby="fbk-title">
        <header className="fbk-hd">
          <h2 id="fbk-title">{t("反馈")}</h2>
          <button className="btn sm" data-action="feedback.close" onClick={onClose} aria-label={t("关闭")}>
            <StudioIcon name="close" />
            <span className="esc">Esc</span>
          </button>
        </header>
        <div className="fbk-tabs" role="tablist" aria-label={t("反馈")}>
          {TABS.map((id) => (
            <button
              key={id}
              id={`fbk-tab-${id}`}
              role="tab"
              type="button"
              aria-selected={tab === id}
              aria-controls={`fbk-panel-${id}`}
              tabIndex={tab === id ? 0 : -1}
              data-action-click="feedback.tab"
              data-action-keydown="feedback.tab"
              data-value={id}
              onClick={() => setTab(id)}
              onKeyDown={(e) => walk(e, id)}
            >
              {t(TAB_LABEL[id])}
            </button>
          ))}
        </div>
        <div className="fbk-body">
          <div id="fbk-panel-send" role="tabpanel" aria-labelledby="fbk-tab-send" hidden={tab !== "send"}>
            <FeedbackForm port={port} onMine={() => setTab("mine")} onClose={onClose} onFile={openLink} />
          </div>
          {tab === "mine" && (
            <div id="fbk-panel-mine" role="tabpanel" aria-labelledby="fbk-tab-mine">
              <FeedbackMine port={port} onFile={openLink} onUnread={onUnread} />
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
