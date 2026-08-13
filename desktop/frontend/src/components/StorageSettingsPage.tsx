import { useCallback, useEffect, useState, type ReactNode } from "react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";

type StorageSettingsView = Awaited<ReturnType<typeof app.StorageSettings>>;

export function StorageSettingsPage() {
  const t = useT();
  const [view, setView] = useState<StorageSettingsView | null>(null);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setFailed(false);
    try {
      setView(await app.StorageSettings());
    } catch {
      setView(null);
      setFailed(true);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  if (loading) return <div className="empty">{t("settings.loading")}</div>;
  if (failed || !view) {
    return (
      <div className="banner banner--error settings-load-error" role="alert">
        <span>{t("settings.loadFailed")}</span>
        <button className="btn btn--small" type="button" onClick={() => void load()}>{t("common.retry")}</button>
      </div>
    );
  }

  return (
    <section className="settings-section">
      <div className="settings-section__head">
        <div>
          <div className="settings-section__title">{t("settings.storageTitle")}</div>
          <div className="settings-section__desc">{t("settings.storageHint")}</div>
        </div>
      </div>
      <div className="settings-section__body">
        <StoragePathField label={t("settings.defaultWorkspace")} hint={t("settings.defaultWorkspaceHint")} value={view.defaultWorkspace} />
        <StoragePathField label={t("settings.storageState")} value={view.statePath} />
        <StoragePathField label={t("settings.storageCache")} value={view.cachePath} />
        <StoragePathField label={t("settings.storageExtensions")} value={view.extensionsPath} />
      </div>
    </section>
  );
}

function StoragePathField({ label, hint, value }: { label: ReactNode; hint?: ReactNode; value: string }) {
  const t = useT();
  return (
    <div className="settings-field">
      <div className="settings-field__copy">
        <div className="settings-field__label">{label}</div>
        {hint && <div className="settings-field__hint"><span className="settings-field__hint-line">{hint}</span></div>}
      </div>
      <div className="settings-field__control">
        <input className="mem-input" value={value} placeholder={t("common.none")} aria-label={String(label)} readOnly />
      </div>
    </div>
  );
}
