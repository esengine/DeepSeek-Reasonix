import { lazy, Suspense, useEffect, useState, useSyncExternalStore } from "react";
import { onOfficePreviewChange, previewsOffice } from "../../state/prefs";
import type { AgentPort } from "../../port/port";
import { t } from "../../i18n";
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
      <button data-action="document.zoom" data-target={path} onClick={() => setZoom(Math.max(0.25, zoom - 0.25))} aria-label={t("缩小")}>−</button>
      <button data-action="document.zoom" data-target={path} onClick={() => setZoom(1)}>{Math.round(zoom * 100)}%</button>
      <button data-action="document.zoom" data-target={path} onClick={() => setZoom(Math.min(4, zoom + 0.25))} aria-label={t("放大")}>+</button>
    </div>
    {failed ? <p role="alert">{t("图片不可用")}</p> : <div className="document-picture">
      {url && <img src={url} alt={path} style={{ width: `${zoom * 100}%`, maxWidth: "none" }} onError={() => setFailed(true)} />}
    </div>}
  </>;
}

export default function DocumentPreview({ port, path, shown, refresh, onOpenExternal, view }: {
  port: AgentPort; path: string; shown: boolean; refresh: string; onOpenExternal?: () => void;
  view: DocumentViewState;
}) {
  const { value, reload } = useDocument(port, path, shown, refresh);
  const officeOn = useSyncExternalStore(onOfficePreviewChange, previewsOffice, previewsOffice);
  const { info, data, error } = value ?? {};
  const officeWaiting = officeOn && info?.kind === "office" && !data && !error;
  useEffect(() => { if (officeWaiting) reload(); }, [officeWaiting, reload]);
  const fallback = <p role="alert">{t("此文件暂不支持预览")}</p>;
  let body = <p role="status">{t("正在读取…")}</p>;
  if (error) body = <p role="alert">{error}</p>;
  else if (info?.kind === "unsupported") body = fallback;
  else if (info?.kind === "office" && !officeOn) body = <p role="status">{t("Office 文档预览已在设置中关闭")}</p>;
  else if (info && info.size > info.limit) body = <p role="alert">{t("文件超过预览大小限制")}</p>;
  else if (data && info) {
    const text = () => new TextDecoder("utf-8", { fatal: true }).decode(data);
    switch (info.kind) {
      case "pdf": body = <PdfPreview data={data} view={view} />; break;
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
      <button data-action="document.refresh" data-target={path} onClick={reload}>{t("刷新")}</button>
      {onOpenExternal && <button data-action="workbench.open-file" data-target={path} onClick={onOpenExternal}>{t("用系统应用打开")}</button>}
    </div>
    <Boundary fallback={fallback} retryKey={info?.version}>
      <Suspense fallback={<p role="status">{t("正在读取…")}</p>}>{body}</Suspense>
    </Boundary>
  </div>;
}
