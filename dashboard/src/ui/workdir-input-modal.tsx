import { useCallback, useEffect, useRef, useState } from "react";
import { I } from "../icons";
import { t, useLang } from "../i18n";

interface BrowseEntry {
  name: string;
  full: string;
}

interface BrowseResult {
  path: string;
  parent: string | null;
  entries: BrowseEntry[];
}

async function fetchBrowse(path: string): Promise<BrowseResult> {
  const token = document
    .querySelector('meta[name="reasonix-token"]')
    ?.getAttribute("content") ?? "";
  const params = new URLSearchParams();
  if (path) params.set("path", path);
  if (token && token !== "__REASONIX_TOKEN__") params.set("token", token);
  const res = await fetch(`/api/browse?${params.toString()}`);
  if (!res.ok) {
    const text = await res.text();
    throw new Error(text || `browse failed (${res.status})`);
  }
  return (await res.json()) as BrowseResult;
}

export function WorkdirInputModal({
  open,
  initialPath,
  onCancel,
  onConfirm,
}: {
  open: boolean;
  initialPath?: string;
  onCancel: () => void;
  onConfirm: (path: string) => void;
}) {
  useLang();
  const [typed, setTyped] = useState<string>(initialPath ?? "");
  const [browse, setBrowse] = useState<BrowseResult | null>(null);
  const [error, setError] = useState<string>("");
  const [loading, setLoading] = useState(false);
  const seededRef = useRef(false);

  const navigate = useCallback(async (path: string) => {
    setLoading(true);
    setError("");
    try {
      const result = await fetchBrowse(path);
      setBrowse(result);
      setTyped(result.path);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!open) {
      seededRef.current = false;
      return;
    }
    if (seededRef.current) return;
    seededRef.current = true;
    void navigate(initialPath ?? "");
  }, [open, initialPath, navigate]);

  if (!open) return null;

  const trimmedTyped = typed.trim();
  const canConfirm = trimmedTyped.length > 0 && !loading;
  const sameAsCurrent = browse !== null && trimmedTyped === browse.path;

  const handleConfirm = () => {
    if (!canConfirm) return;
    onConfirm(trimmedTyped);
  };

  const handleTypedKey = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Escape") {
      e.preventDefault();
      onCancel();
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (sameAsCurrent) {
        handleConfirm();
      } else {
        void navigate(trimmedTyped);
      }
    }
  };

  return (
    <div className="settings-mask" onMouseDown={onCancel}>
      <div
        className="workdir-input"
        onMouseDown={(e) => e.stopPropagation()}
      >
        <div className="workdir-input-head">
          <I.folder size={14} />
          <span>{t("workdir.title")}</span>
          <button
            type="button"
            className="close-btn"
            onClick={onCancel}
            title={t("workdir.cancel")}
          >
            <I.x size={13} />
          </button>
        </div>
        <div className="workdir-input-pathrow">
          <input
            autoFocus
            className="workdir-input-path"
            value={typed}
            placeholder={t("workdir.pathPlaceholder")}
            onChange={(e) => setTyped(e.target.value)}
            onKeyDown={handleTypedKey}
            spellCheck={false}
          />
          <button
            type="button"
            className="btn ghost"
            onClick={() => void navigate(trimmedTyped)}
            disabled={loading || trimmedTyped.length === 0}
            title={t("workdir.goTip")}
          >
            {t("workdir.go")}
          </button>
        </div>
        {error ? <div className="workdir-input-error">{error}</div> : null}
        <div className="workdir-input-list">
          {browse?.parent ? (
            <div
              className="workdir-input-row up"
              onClick={() => void navigate(browse.parent!)}
              role="button"
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") void navigate(browse.parent!);
              }}
            >
              <span className="ic">↑</span>
              <span>..</span>
            </div>
          ) : null}
          {browse?.entries.length === 0 && !loading ? (
            <div className="workdir-input-empty">{t("workdir.emptyDir")}</div>
          ) : null}
          {browse?.entries.map((e) => (
            <div
              key={e.full}
              className="workdir-input-row"
              onClick={() => void navigate(e.full)}
              onDoubleClick={() => onConfirm(e.full)}
              role="button"
              tabIndex={0}
              title={e.full}
              onKeyDown={(ev) => {
                if (ev.key === "Enter") void navigate(e.full);
              }}
            >
              <span className="ic"><I.folder size={12} /></span>
              <span className="n">{e.name}</span>
            </div>
          ))}
          {loading ? <div className="workdir-input-empty">{t("workdir.loading")}</div> : null}
        </div>
        <div className="workdir-input-foot">
          <button type="button" className="btn ghost" onClick={onCancel}>
            {t("workdir.cancel")}
          </button>
          <button
            type="button"
            className="btn primary"
            onClick={handleConfirm}
            disabled={!canConfirm}
          >
            {t("workdir.openHere")}
          </button>
        </div>
      </div>
    </div>
  );
}
