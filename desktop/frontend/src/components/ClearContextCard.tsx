import { useEffect, useRef } from "react";
import { useT } from "../lib/i18n";
import { PromptAction, PromptBadge, PromptShelf } from "./PromptShelf";

export function ClearContextCard({
  onCancel,
  onConfirm,
}: {
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const t = useT();
  const shelfRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    shelfRef.current?.focus();
  }, []);

  useEffect(() => {
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      const tag = target?.tagName.toLowerCase();
      if (tag === "input" || tag === "textarea" || target?.isContentEditable) return;

      if (event.key === "Escape" || event.key === "1") {
        event.preventDefault();
        onCancel();
        return;
      }
      if (event.key === "2" || event.key.toLowerCase() === "y") {
        event.preventDefault();
        onConfirm();
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [onCancel, onConfirm]);

  return (
    <PromptShelf
      barRef={shelfRef}
      titleId="clear-context-shelf-title"
      title={t("clearContext.title")}
      badges={<PromptBadge>{t("clearContext.badge")}</PromptBadge>}
      meta={t("clearContext.prompt")}
      actions={
        <>
          <PromptAction keyLabel="1" label={t("common.cancel")} selected onClick={onCancel} />
          <PromptAction keyLabel="2" label={t("clearContext.clear")} onClick={onConfirm} />
        </>
      }
    >
      <p className="prompt-shelf__note">{t("clearContext.detail")}</p>
    </PromptShelf>
  );
}
