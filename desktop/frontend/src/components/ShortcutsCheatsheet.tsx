import { useEffect, useMemo, useRef } from "react";
import {
  resolvedShortcutCombo,
  shortcutDefinitions,
  type ShortcutPlatform,
  type ShortcutSection,
} from "../lib/keyboardShortcuts";
import type { DictKey, Translator } from "../lib/i18n";
import { ModalCloseButton } from "./ModalCloseButton";
import { ShortcutComboDisplay } from "./ShortcutComboDisplay";

const SECTION_ORDER: ShortcutSection[] = ["global", "session", "view", "tools", "help"];
const SECTION_LABEL_KEYS: Record<ShortcutSection, DictKey> = {
  global: "shortcuts.section.global",
  session: "shortcuts.section.session",
  view: "shortcuts.section.view",
  tools: "shortcuts.section.tools",
  help: "shortcuts.section.help",
};

export function ShortcutsCheatsheet({
  open,
  platform,
  onClose,
  t,
}: {
  open: boolean;
  platform: ShortcutPlatform;
  onClose: () => void;
  t: Translator;
}) {
  const closeRef = useRef<HTMLButtonElement>(null);
  const restoreFocusRef = useRef<HTMLElement | null>(null);
  const groups = useMemo(() => {
    return SECTION_ORDER.map((section) => ({
      section,
      items: shortcutDefinitions().filter((definition) => definition.section === section),
    })).filter((group) => group.items.length > 0);
  }, []);

  useEffect(() => {
    if (open) {
      restoreFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      requestAnimationFrame(() => closeRef.current?.focus());
      return;
    }
    if (restoreFocusRef.current?.isConnected) restoreFocusRef.current.focus();
    restoreFocusRef.current = null;
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      onClose();
    };
    document.addEventListener("keydown", onKey, { capture: true });
    return () => document.removeEventListener("keydown", onKey, { capture: true });
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div className="drawer-backdrop shortcuts-cheatsheet-backdrop" onClick={onClose} role="presentation">
      <aside
        className="drawer drawer--wide shortcuts-cheatsheet"
        role="dialog"
        aria-modal="true"
        aria-labelledby="shortcuts-cheatsheet-title"
        onClick={(event) => event.stopPropagation()}
      >
        <header className="drawer__head">
          <div>
            <div id="shortcuts-cheatsheet-title" className="drawer__title">
              {t("shortcuts.cheatsheetTitle")}
            </div>
            <div className="drawer__summary">{t("shortcuts.cheatsheetSummary")}</div>
          </div>
          <ModalCloseButton ref={closeRef} label={t("common.close")} onClick={onClose} />
        </header>
        <div className="drawer__body shortcuts-cheatsheet__body">
          {groups.map((group) => (
            <section className="shortcuts-cheatsheet__section" key={group.section}>
              <h3>{t(SECTION_LABEL_KEYS[group.section])}</h3>
              <div className="shortcuts-cheatsheet__list">
                {group.items.map((definition) => (
                  <div className="shortcuts-cheatsheet__row" key={definition.action}>
                    <ShortcutComboDisplay
                      as="kbd"
                      combo={resolvedShortcutCombo(definition.action, platform)}
                      platform={platform}
                    />
                    <div>
                      <strong>{t(definition.labelKey)}</strong>
                      <span className="shortcuts-cheatsheet__desc">{t(definition.descriptionKey)}</span>
                    </div>
                  </div>
                ))}
              </div>
            </section>
          ))}
        </div>
      </aside>
    </div>
  );
}
