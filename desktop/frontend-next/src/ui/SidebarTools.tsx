import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState, type KeyboardEvent } from "react";
import { createPortal } from "react-dom";
import { Brain, ChartColumn, ChevronUp, Files, Plug, Server, Settings, ShieldCheck } from "lucide-react";
import { t } from "../i18n";
import type { ApprovalMode } from "../port/port";
import { approvalName } from "./approvals";
import { useDismiss } from "./dismiss";
import { chord } from "./keys";
import { useDestinations } from "./navdest";
import { beside, pinToViewport, useFollow } from "./place";
import { UpdateDot } from "./UpdateDot";
import { MASK, useHidesAmounts } from "./wallet";

const ICONS = { usage: ChartColumn, files: Files, tools: ShieldCheck, ext: Plug, memory: Brain, remote: Server };

export function SidebarTools({ wallet, approval, online, onSettings, onFiles }: {
  wallet: string; approval?: ApprovalMode; online: number; onSettings: (section?: string) => void; onFiles: () => void;
}) {
  const destinations = useDestinations();
  const hideAmounts = useHidesAmounts();
  const [openAt, setOpenAt] = useState<"pointer" | "first" | "last" | null>(null);
  const open = openAt !== null;
  const menuId = useId();
  const root = useRef<HTMLDivElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const toggle = useRef<HTMLButtonElement>(null);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const cancel = useCallback(() => clearTimeout(timer.current), []);
  const close = useCallback(() => {
    cancel();
    if (menu.current?.contains(document.activeElement)) toggle.current?.focus();
    setOpenAt(null);
  }, [cancel]);
  useDismiss(open, root, close, menu);
  useEffect(() => cancel, [cancel]);
  const enter = () => {
    cancel();
    if (destinations.length) timer.current = setTimeout(() => setOpenAt((value) => value ?? "pointer"), 160);
  };
  const leave = () => {
    cancel();
    if (!menu.current?.contains(document.activeElement)) timer.current = setTimeout(close, 180);
  };
  const place = useCallback(() => {
    if (!root.current || !menu.current) return;
    const anchor = root.current.getBoundingClientRect();
    pinToViewport(menu.current, anchor.left, beside(anchor, menu.current.getBoundingClientRect().height, innerHeight, "above", 6));
  }, []);
  useFollow(open, place, destinations.length, { anchor: root, container: menu });
  useLayoutEffect(() => {
    if (openAt !== "first" && openAt !== "last") return;
    const buttons = menu.current?.querySelectorAll<HTMLButtonElement>("button");
    (openAt === "last" ? buttons?.[buttons.length - 1] : buttons?.[0])?.focus();
  }, [openAt]);
  const key = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === "Tab") { close(); return; }
    const step = { ArrowDown: 1, ArrowUp: -1, ArrowRight: 1, ArrowLeft: -1, Home: -Infinity, End: Infinity }[event.key];
    if (step === undefined) return;
    event.preventDefault();
    const buttons = [...event.currentTarget.querySelectorAll<HTMLButtonElement>("button:not(:disabled)")];
    const at = buttons.indexOf(document.activeElement as HTMLButtonElement);
    const next = step === Infinity ? buttons.length - 1 : step === -Infinity ? 0 : (at + step + buttons.length) % buttons.length;
    buttons[next]?.focus();
  };
  const settings = () => { close(); onSettings(); };
  return (
    <div className="studio-tools" ref={root} onPointerEnter={(event) => { if (event.pointerType !== "touch") enter(); }} onPointerLeave={leave}>
      <div className="studio-tools-row">
        <button className="studio-settings" data-action="chrome.settings" onClick={settings} aria-label={t("common.settings")} title={`${t("common.settings")} ${chord(",")}`}>
          <Settings size={16} /><span>{t("common.settings")}</span><UpdateDot />
        </button>
        {destinations.length > 0 && <button ref={toggle} className="studio-tools-toggle" data-action-click="sidebar.tools" data-action-keydown="sidebar.tools" aria-label={t("session.sidebar_shortcuts")}
          aria-haspopup="menu" aria-controls={menuId} aria-expanded={open} onClick={(event) => {
            cancel(); setOpenAt(open ? null : event.detail === 0 ? "first" : "pointer");
          }}
          onKeyDown={(event) => {
            if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
            event.preventDefault(); cancel(); setOpenAt(event.key === "ArrowUp" ? "last" : "first");
          }}><ChevronUp size={16} /></button>}
      </div>
      {destinations.length > 0 && createPortal(
        <div className="studio-quicknav" ref={menu} id={menuId} role="menu" aria-label={t("session.sidebar_shortcuts")} hidden={!open}
          onPointerEnter={cancel} onPointerLeave={leave} onKeyDown={key}>
          {destinations.map(([mark, label, section]) => {
            const Icon = ICONS[mark as keyof typeof ICONS];
            const isFiles = mark === "files";
            const reading = mark === "usage" ? wallet ? hideAmounts ? MASK : wallet : ""
              : mark === "remote" && online > 0 ? t("common.n_online", { n: online })
              : mark === "tools" && approval ? approvalName(approval) : "";
            return <button key={mark} role="menuitem" tabIndex={-1} className={mark === "usage" ? "studio-wallet" : undefined}
              data-action={isFiles ? "files.open" : "settings.section"} data-value={mark === "files" ? undefined : section}
              aria-label={reading ? `${t(label)} · ${reading}` : t(label)}
              onClick={() => { close(); if (mark === "files") onFiles(); else onSettings(mark === "usage" ? "spend" : mark === "ext" ? "extensions" : section); }}>
              <Icon size={16} /><span>{t(label)}</span>{reading && <small>{reading}</small>}
            </button>;
          })}
        </div>, document.body,
      )}
    </div>
  );
}
