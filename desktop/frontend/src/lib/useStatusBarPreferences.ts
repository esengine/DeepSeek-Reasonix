import { useCallback, useRef, useState } from "react";
import { app } from "./bridge";
import { useI18n } from "./i18n";
import { useToast } from "./toast";
import { DEFAULT_STATUS_BAR_ITEMS, normalizeStatusBarItems } from "./statusBarItems";
import type { DesktopStartupSettingsView } from "./types";

export function useStatusBarPreferences() {
  const [style, setStyle] = useState<"icon" | "text">("text");
  const [items, setItems] = useState(DEFAULT_STATUS_BAR_ITEMS);
  const [hidden, setHidden] = useState<boolean>();
  const [pending, setPending] = useState(false);
  const saving = useRef(false);
  const { showToast } = useToast();
  const { t } = useI18n();
  const hydrate = useCallback((settings: Pick<DesktopStartupSettingsView, "hideAmounts" | "statusBarStyle" | "statusBarItems">) => {
    setStyle(settings.statusBarStyle === "text" ? "text" : "icon");
    setItems(normalizeStatusBarItems(settings.statusBarItems));
    if (!saving.current) setHidden(settings.hideAmounts === true);
  }, []);

  const toggle = async () => {
    if (hidden === undefined || saving.current) return;
    const next = !hidden;
    saving.current = true;
    setPending(true);
    // Hide immediately; only reveal once the preference is safely persisted.
    setHidden(true);
    try {
      await app.SetHideAmounts(next);
      setHidden(next);
    } catch (error) {
      showToast(`${t("status.amountsSaveFailed")}: ${error instanceof Error ? error.message : String(error)}`, "error");
    } finally {
      saving.current = false;
      setPending(false);
    }
  };

  return { style, items, hidden: hidden !== false, pending: hidden === undefined || pending, hydrate, toggle };
}
