import { openUrl } from "@tauri-apps/plugin-opener";
import { t } from "../i18n";
import { I } from "../icons";

const REPO_URL = "https://github.com/esengine/DeepSeek-Reasonix";

export function AboutModal({ onClose }: { onClose: () => void }) {
  const openGitHub = () => {
    void openUrl(REPO_URL).catch(() => undefined);
  };

  return (
    <div className="about-mask" onClick={onClose}>
      <div className="about-modal" onClick={(e) => e.stopPropagation()}>
        <button type="button" className="about-close" onClick={onClose} aria-label={t("about.close")}>
          <I.x size={14} />
        </button>
        <div className="about-brand">
          <div className="about-name">Reasonix</div>
          <div className="about-tagline">{t("about.tagline")}</div>
        </div>
        <div className="about-meta">
          <div className="about-row">
            <span className="about-label">{t("about.version")}</span>
            <code className="about-value">{__APP_VERSION__}</code>
          </div>
          <div className="about-row">
            <span className="about-label">{t("about.repo")}</span>
            <button type="button" className="about-link" onClick={openGitHub}>
              <I.link size={12} />
              <span>esengine/DeepSeek-Reasonix</span>
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
