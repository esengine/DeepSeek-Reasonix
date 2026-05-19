import { openUrl } from "@tauri-apps/plugin-opener";
import { check as checkUpdate } from "@tauri-apps/plugin-updater";
import { useCallback, useState } from "react";
import { t } from "../i18n";
import { I } from "../icons";

const REPO_URL = "https://github.com/esengine/DeepSeek-Reasonix";
const RELEASES_PAGE = `${REPO_URL}/releases`;

type CheckState =
  | { kind: "idle" }
  | { kind: "checking" }
  | { kind: "up-to-date"; latest: string }
  | { kind: "outdated"; latest: string }
  | { kind: "error"; message: string };

export function AboutModal({ onClose }: { onClose: () => void }) {
  const [check, setCheck] = useState<CheckState>({ kind: "idle" });

  const openGitHub = useCallback(() => {
    void openUrl(REPO_URL).catch(() => undefined);
  }, []);
  const openReleases = useCallback(() => {
    void openUrl(RELEASES_PAGE).catch(() => undefined);
  }, []);

  const checkForUpdates = useCallback(async () => {
    setCheck({ kind: "checking" });
    try {
      const update = await checkUpdate();
      if (!update) {
        setCheck({ kind: "up-to-date", latest: __APP_VERSION__ });
      } else {
        setCheck({ kind: "outdated", latest: update.version });
      }
    } catch (err) {
      setCheck({ kind: "error", message: (err as Error).message });
    }
  }, []);

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
        <div className="about-actions">
          <button
            type="button"
            className="about-check"
            onClick={checkForUpdates}
            disabled={check.kind === "checking"}
          >
            <I.rotate size={12} />
            <span>{check.kind === "checking" ? t("about.checking") : t("about.checkUpdates")}</span>
          </button>
          <CheckStatus check={check} onOpenReleases={openReleases} />
        </div>
      </div>
    </div>
  );
}

function CheckStatus({
  check,
  onOpenReleases,
}: { check: CheckState; onOpenReleases: () => void }) {
  if (check.kind === "idle" || check.kind === "checking") return null;
  if (check.kind === "up-to-date") {
    return (
      <div className="about-status ok">
        <I.check size={12} />
        <span>{t("about.upToDate", { version: check.latest })}</span>
      </div>
    );
  }
  if (check.kind === "outdated") {
    return (
      <div className="about-status warn">
        <span>{t("about.updateAvailable", { version: check.latest })}</span>
        <button type="button" className="about-link" onClick={onOpenReleases}>
          <I.download size={12} />
          <span>{t("about.openReleases")}</span>
        </button>
      </div>
    );
  }
  return (
    <div className="about-status err">
      <span>{t("about.checkFailed", { message: check.message })}</span>
    </div>
  );
}
