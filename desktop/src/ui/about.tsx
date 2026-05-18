import { openUrl } from "@tauri-apps/plugin-opener";
import { useCallback, useState } from "react";
import { t } from "../i18n";
import { I } from "../icons";

const REPO_URL = "https://github.com/esengine/DeepSeek-Reasonix";
const RELEASES_API = "https://api.github.com/repos/esengine/DeepSeek-Reasonix/releases";
const RELEASES_PAGE = `${REPO_URL}/releases`;

type CheckState =
  | { kind: "idle" }
  | { kind: "checking" }
  | { kind: "up-to-date"; latest: string }
  | { kind: "outdated"; latest: string }
  | { kind: "error"; message: string };

/** Lexicographic-but-numeric semver compare. Returns 1 if a > b, -1 if a < b, 0 if equal. */
function cmpSemver(a: string, b: string): number {
  const pa = a.split(/[.+-]/).map((s) => Number.parseInt(s, 10));
  const pb = b.split(/[.+-]/).map((s) => Number.parseInt(s, 10));
  const len = Math.max(pa.length, pb.length);
  for (let i = 0; i < len; i++) {
    const av = Number.isFinite(pa[i]) ? (pa[i] as number) : 0;
    const bv = Number.isFinite(pb[i]) ? (pb[i] as number) : 0;
    if (av !== bv) return av > bv ? 1 : -1;
  }
  return 0;
}

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
      const resp = await fetch(RELEASES_API, {
        headers: { Accept: "application/vnd.github+json" },
      });
      if (!resp.ok) {
        setCheck({ kind: "error", message: `HTTP ${resp.status}` });
        return;
      }
      const releases = (await resp.json()) as Array<{ tag_name?: string; draft?: boolean; prerelease?: boolean }>;
      // Desktop releases live under the `desktop-v*` tag namespace (#1153).
      // Fall back to bare `v*` so the check still works before the desktop
      // track has cut its own tag.
      const stable = releases.filter((r) => !r.draft && !r.prerelease);
      const desktopTag = stable.find((r) => r.tag_name?.startsWith("desktop-v"))?.tag_name;
      const fallbackTag = stable.find((r) => r.tag_name?.startsWith("v"))?.tag_name;
      const tag = desktopTag ?? fallbackTag;
      if (!tag) {
        setCheck({ kind: "error", message: t("about.checkNoRelease") });
        return;
      }
      const latest = tag.replace(/^desktop-v|^v/, "");
      const current = __APP_VERSION__;
      setCheck({
        kind: cmpSemver(latest, current) > 0 ? "outdated" : "up-to-date",
        latest,
      });
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
