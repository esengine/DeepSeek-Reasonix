import { lazy, Suspense, useEffect, useRef, useState } from "react";
import { MoreHorizontal } from "lucide-react";

import { Tooltip } from "./Tooltip";
import type { TopicbarMoreMenuContentProps } from "./TopicbarMoreMenuContent";

import { t } from "../lib/i18n";

const loadMenuContent = () => import("./TopicbarMoreMenuContent");
const TopicbarMoreMenuContent = lazy(async () => ({
  default: (await loadMenuContent()).TopicbarMoreMenuContent,
}));

type TopicbarMoreMenuProps = Omit<TopicbarMoreMenuContentProps, "copied" | "initialFocus" | "onClose" | "onCopied">;

/** Keeps the topic bar compact while loading overflow actions only on demand. */
export function TopicbarMoreMenu(props: TopicbarMoreMenuProps) {
  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const [menuOpen, setMenuOpen] = useState(false);
  const [initialFocus, setInitialFocus] = useState<"first" | "last">("first");
  const [copied, setCopied] = useState(false);
  const copiedTimerRef = useRef<number | null>(null);

  const closeMenu = (restoreFocus: boolean) => {
    setMenuOpen(false);
    if (restoreFocus) triggerRef.current?.focus();
  };

  useEffect(() => {
    if (!menuOpen) return;
    const onPointerDown = (event: MouseEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) closeMenu(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") closeMenu(true);
    };
    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [menuOpen]);

  useEffect(() => () => {
    if (copiedTimerRef.current != null) window.clearTimeout(copiedTimerRef.current);
  }, []);

  const markCopied = () => {
    setCopied(true);
    if (copiedTimerRef.current != null) window.clearTimeout(copiedTimerRef.current);
    copiedTimerRef.current = window.setTimeout(() => setCopied(false), 1200);
  };

  const openFromKeyboard = (event: React.KeyboardEvent<HTMLButtonElement>) => {
    if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
    event.preventDefault();
    setInitialFocus(event.key === "ArrowUp" ? "last" : "first");
    setMenuOpen(true);
  };

  return (
    <div ref={rootRef} className={`topicbar__more${menuOpen ? " topicbar__more--open" : ""}`}>
      <Tooltip label={t("topicBar.more")}>
        <button
          ref={triggerRef}
          className="topicbar__action-btn topicbar__action-btn--icon topicbar__action-btn--utility"
          type="button"
          aria-label={t("topicBar.more")}
          aria-haspopup="menu"
          aria-expanded={menuOpen}
          onPointerEnter={() => { void loadMenuContent(); }}
          onFocus={() => { void loadMenuContent(); }}
          onKeyDown={openFromKeyboard}
          onClick={() => {
            setInitialFocus("first");
            setMenuOpen((open) => !open);
          }}
        >
          <MoreHorizontal size={15} />
        </button>
      </Tooltip>
      {menuOpen && (
        <Suspense fallback={null}>
          <TopicbarMoreMenuContent
            {...props}
            copied={copied}
            initialFocus={initialFocus}
            onClose={closeMenu}
            onCopied={markCopied}
          />
        </Suspense>
      )}
    </div>
  );
}
