import { lazy, Suspense, useEffect, useState, useSyncExternalStore } from "react";
import { onOfficePreviewChange, previewsOffice } from "../../state/prefs";
import type { AgentPort } from "../../port/port";
import { t } from "../../i18n";
import { bytes } from "../../i18n/format";
import { WorkbenchState } from "../WorkbenchState";
import { useDocument } from "./useDocument";
import { Boundary } from "../Boundary";
import "./document.css";
import type { DocumentViewState } from "./viewState";

const PdfPreview = lazy(() => import("./PdfPreview"));
const DelimitedPreview = lazy(() => import("./DelimitedPreview"));
const StaticHtmlPreview = lazy(() => import("./StaticHtmlPreview"));
const OfficePreview = lazy(() => import("./OfficePreview"));

function Picture({ data, path, view }: { data: ArrayBuffer; path: string; view: DocumentViewState }) {
  const [url, setURL] = useState("");
  const [zoom, updateZoom] = useState(view.zoom ?? 1);
  const setZoom = (value: number) => { view.zoom = value; updateZoom(value); };
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    const type = /\.svg$/i.test(path) ? "image/svg+xml" : undefined;
    const url = URL.createObjectURL(new Blob([data], { type }));
    setURL(url); setFailed(false);
    return () => URL.revokeObjectURL(url);
  }, [data, path]);
  return <>
    <div className="document-controls">
      <button data-action="document.zoom" data-target={path} onClick={() => setZoom(Math.max(0.25, zoom - 0.25))} aria-label={t("chat.zoom_out")}>−</button>
      <button data-action="document.zoom" data-target={path} onClick={() => setZoom(1)}>{Math.round(zoom * 100)}%</button>
      <button data-action="document.zoom" data-target={path} onClick={() => setZoom(Math.min(4, zoom + 0.25))} aria-label={t("chat.zoom_in")}>+</button>
    </div>
    {failed ? <p role="alert">{t("chat.image_unavailable")}</p> : <div className="document-picture">
      {url && <img src={url} alt={path} style={{ width: `${zoom * 100}%`, maxWidth: "none" }} onError={() => setFailed(true)} />}
    </div>}
  </>;
}

export default function DocumentPreview({ port, path, shown, refresh, onOpenExternal, onSettings, view }: {
  port: AgentPort; path: string; shown: boolean; refresh: string; onOpenExternal?: () => void; onSettings?: (at?: string) => void;
  view: DocumentViewState;
}) {
  const { value, reload } = useDocument(port, path, shown, refresh);
  const officeOn = useSyncExternalStore(onOfficePreviewChange, previewsOffice, previewsOffice);
  const { info, data, error } = value ?? {};
  const officeWaiting = officeOn && info?.kind === "office" && !data && !error;
  useEffect(() => { if (officeWaiting) reload(); }, [officeWaiting, reload]);
  const fallback = <p role="alert">{t("chat.preview_is_not_available_for")}</p>;
  let body = <p role="status">{t("common.loading")}</p>;
  if (error) body = <p role="alert">{error}</p>;
  else if (info?.kind === "unsupported") body = fallback;
  else if (info?.kind === "office" && !officeOn) {
    body = (
      <div className="office-off" role="status">
        <p><b>{path.split("/").pop()}</b> · {t("chat.office_preview_is_off")}</p>
        {(onOpenExternal || onSettings) && <div>
          {onOpenExternal && <button type="button" className="office-off-open" data-action="workbench.open-file" data-target={path} onClick={onOpenExternal}>{t("chat.open_in_system_application")}</button>}
          {onSettings && <button type="button" className="office-off-go" data-action="settings.section" data-value="appearance" onClick={() => onSettings("appearance")}>{t("chat.turn_preview_on_in_settings")}</button>}
        </div>}
      </div>
    );
  }
  else if (info && info.size > info.limit) {
    body = (
      <WorkbenchState icon="layers" title={t("workbench.file_too_large_to_preview")} detail={t("workbench.size_preview_limit", { size: bytes(info.size), limit: bytes(info.limit) })} alert>
        {onOpenExternal && <button type="button" data-action="workbench.open-file" data-target={path} onClick={onOpenExternal}>{t("chat.open_in_system_application")}</button>}
      </WorkbenchState>
    );
  }
  else if (data && info) {
    const text = () => new TextDecoder("utf-8", { fatal: true }).decode(data);
    switch (info.kind) {
      case "pdf": body = <PdfPreview data={data} view={view} onOpenExternal={onOpenExternal} />; break;
      case "table": body = <DelimitedPreview path={path} text={text()} view={view} />; break;
      case "html": body = <StaticHtmlPreview text={text()} />; break;
      case "image": body = <Picture data={data} path={path} view={view} />; break;
      case "office": body = <OfficePreview data={data} path={path} view={view} />; break;
      default: body = fallback;
    }
  }
  return <div className="document-preview">
    <div className="document-controls">
      {info && <span>{info.size.toLocaleString()} B</span>}
      <button data-action="document.refresh" data-target={path} onClick={reload}>{t("common.re_freeze")}</button>
      {onOpenExternal && !(info && info.size > info.limit) && !(info?.kind === "office" && !officeOn) && <button data-action="workbench.open-file" data-target={path} onClick={onOpenExternal}>{t("chat.open_in_system_application")}</button>}
    </div>
    <Boundary fallback={fallback} retryKey={info?.version}>
      <Suspense fallback={<p role="status">{t("common.loading")}</p>}>{body}</Suspense>
    </Boundary>
  </div>;
}
