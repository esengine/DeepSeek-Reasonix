import { useCallback, useState } from "react";
import { ExternalLink } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { DockTab } from "../lib/types";

interface BrowserPanelProps {
  tab: DockTab;
  onMetadataUpdate: (id: string, meta: Record<string, unknown>) => void;
  onTitleUpdate: (id: string, title: string) => void;
}

export function BrowserPanel({ tab }: BrowserPanelProps) {
  const t = useT();
  const meta = (tab.metadata ?? { url: "about:blank" }) as { url: string };
  const [urlInput, setUrlInput] = useState(meta.url === "about:blank" ? "" : meta.url);

  const openInBrowser = useCallback(
    (targetUrl: string) => {
      let normalized = targetUrl.trim();
      if (!normalized) return;
      if (!/^https?:\/\//i.test(normalized)) {
        normalized = `https://${normalized}`;
      }
      setUrlInput(normalized);
      void app.OpenURL(normalized);
    },
    [],
  );

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key === "Enter") {
        e.preventDefault();
        openInBrowser(urlInput);
      }
    },
    [openInBrowser, urlInput],
  );

  return (
    <div className="browser-panel">
      <div className="browser-nav">
        <div className="browser-url">
          <input
            className="browser-url__input"
            type="text"
            value={urlInput}
            onChange={(e) => setUrlInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={t("rightDock.browserUrlPlaceholder")}
          />
        </div>
        <button
          className="browser-nav__btn browser-nav__btn--go"
          type="button"
          aria-label={t("rightDock.browserOpen")}
          onClick={() => openInBrowser(urlInput)}
        >
          <ExternalLink size={14} />
        </button>
      </div>
      <div className="browser-empty">
        <div className="browser-empty__hint">{t("rightDock.browserEmpty")}</div>
      </div>
    </div>
  );
}
