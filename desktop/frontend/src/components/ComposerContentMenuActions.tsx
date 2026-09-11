import { AtSign, FilePlus2, Hash } from "lucide-react";
import { useT } from "../lib/i18n";

export function ComposerContentMenuActions({
  attachmentInputEnabled,
  textPresent,
  onChooseAttachment,
  onInsertTrigger,
}: {
  attachmentInputEnabled: boolean;
  textPresent: boolean;
  onChooseAttachment: () => void;
  onInsertTrigger: (trigger: "@" | "#" | "/") => void;
}) {
  const t = useT();
  return (
    <div className="composer-access-menu__section" role="menu" aria-label={t("composer.contentMenuTitle")}>
      <div className="composer-access-menu__label">{t("composer.contentMenuTitle")}</div>
      {attachmentInputEnabled ? <>
        <button type="button" role="menuitem" className="composer-access-menu__item composer-content-menu__item" onClick={onChooseAttachment}>
          <FilePlus2 size={16} aria-hidden="true" />
          <span className="composer-access-menu__copy">
            <span className="composer-access-menu__title">{t("composer.contentAddAttachment")}</span>
          </span>
        </button>
        <button type="button" role="menuitem" className="composer-access-menu__item composer-content-menu__item" onClick={() => onInsertTrigger("@")}>
          <AtSign size={16} aria-hidden="true" />
          <span className="composer-access-menu__copy">
            <span className="composer-access-menu__title">{t("composer.contentReferenceFiles")}</span>
          </span>
        </button>
      </> : null}
      <button type="button" role="menuitem" className="composer-access-menu__item composer-content-menu__item" onClick={() => onInsertTrigger("#")}>
        <Hash size={16} aria-hidden="true" />
        <span className="composer-access-menu__copy">
          <span className="composer-access-menu__title">{t("composer.contentReferenceSessions")}</span>
        </span>
      </button>
      <button type="button" role="menuitem" className="composer-access-menu__item composer-content-menu__item"
        onClick={() => onInsertTrigger("/")} disabled={textPresent}
        title={textPresent ? t("composer.contentUseCommandsEmptyOnly") : undefined}>
        <span className="composer-content-menu__trigger-icon" aria-hidden="true">/</span>
        <span className="composer-access-menu__copy">
          <span className="composer-access-menu__title">{t("composer.contentUseCommands")}</span>
        </span>
      </button>
    </div>
  );
}
