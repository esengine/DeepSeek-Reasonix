import { useEffect, useMemo, useState } from "react";
import { ContextMenu, type ContextMenuPoint } from "./ContextMenu";
import { messageSelectionContextText } from "../lib/messageSelectionCopy";
import { writeClipboardText } from "../lib/clipboard";
import { detectShortcutPlatform, formatShortcutCombo } from "../lib/keyboardShortcuts";
import { useT } from "../lib/i18n";

// Inside the Wails shell main.tsx suppresses the webview's default context
// menu (its Reload/Back/Inspect entries can navigate away from the app), which
// also removes the native Copy menu for selected transcript text — ⌘C still
// works, but the right-click path is dead. This mounts one document-level
// listener that offers an app-drawn Copy menu whenever a suppressed selection
// menu would have applied, gated exactly like the ⌘C interceptor. It stays
// inert in a plain browser (no window.runtime), where the native menu opens.
export function TranscriptSelectionMenu() {
  const t = useT();
  const [point, setPoint] = useState<ContextMenuPoint | null>(null);
  const [text, setText] = useState("");
  const shortcutPlatform = useMemo(() => detectShortcutPlatform(), []);

  useEffect(() => {
    const onContextMenu = (event: MouseEvent) => {
      if (typeof window === "undefined" || !window.runtime) return;
      const selected = messageSelectionContextText(document, event.target);
      if (selected == null) return;
      event.preventDefault();
      setText(selected);
      setPoint(menuPointFromEvent(event));
    };
    document.addEventListener("contextmenu", onContextMenu);
    return () => document.removeEventListener("contextmenu", onContextMenu);
  }, []);

  return (
    <ContextMenu
      open={point != null}
      point={point}
      minWidth={140}
      ariaLabel={t("common.copy")}
      items={[
        {
          key: "copy",
          label: t("common.copy"),
          shortcut: formatShortcutCombo(
            shortcutPlatform === "darwin" ? { key: "c", meta: true } : { key: "c", ctrl: true },
            shortcutPlatform,
          ),
          onSelect: () => {
            void writeClipboardText(text);
            setPoint(null);
          },
        },
      ]}
      onClose={() => setPoint(null)}
    />
  );
}

// The keyboard context-menu key fires contextmenu at (0, 0); anchor the menu
// to the selection instead so it opens next to the highlighted text.
function menuPointFromEvent(event: MouseEvent): ContextMenuPoint {
  if (event.clientX > 0 || event.clientY > 0) {
    return { left: event.clientX, top: event.clientY };
  }
  const range = document.getSelection()?.rangeCount ? document.getSelection()?.getRangeAt(0) : null;
  const rect = typeof range?.getBoundingClientRect === "function" ? range.getBoundingClientRect() : null;
  if (rect && (rect.width > 0 || rect.height > 0)) {
    return { left: rect.left, top: rect.bottom + 4 };
  }
  return { left: 12, top: 12 };
}
