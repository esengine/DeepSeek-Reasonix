import { useEffect, useRef, useState, type MouseEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { useEscape } from "./dismiss";
import { StudioIcon } from "./StudioIcon";
import "./image-preview.css";

export function ImagePreview({ src, name, className, children }: {
  src?: string;
  name: string;
  className?: string;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  useEffect(() => setOpen(false), [src]);
  return <>
    <button type="button" className={["image-trigger", className].filter(Boolean).join(" ")}
      disabled={!src} data-action-click="image.open" data-action-mousedown="image.open" aria-label={t("imagePreview.previewImage", { name })}
      title={t("imagePreview.previewImage", { name })} onClick={() => setOpen(true)}
      onMouseDown={(ev) => { if (ev.detail > 1) ev.preventDefault(); }}>
      {children}
    </button>
    {open && src && <ImageViewer src={src} name={name} onClose={() => setOpen(false)} />}
  </>;
}

function ImageViewer({ src, name, onClose }: { src: string; name: string; onClose: () => void }) {
  const close = useRef<HTMLButtonElement>(null);
  const [failed, setFailed] = useState(false);
  useEscape(true, onClose);
  const backdrop = (ev: MouseEvent<HTMLDivElement>) => {
    if (ev.target === ev.currentTarget && ev.detail < 2) onClose();
  };
  useEffect(() => {
    const previous = document.activeElement;
    const overflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    close.current?.focus();
    return () => {
      document.body.style.overflow = overflow;
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus();
    };
  }, []);

  // Transcript and composer ancestors can scroll or carry backdrop filters;
  // the body portal keeps the viewer outside those containing blocks.
  return createPortal(
    <div className="image-preview" role="dialog" aria-modal="true" aria-label={name}
      data-action-click="image.close" data-action-keydown="image.focus-trap" data-action-mousedown="image.focus-trap"
      onClick={backdrop}
      onMouseDown={(ev) => { if (!close.current?.contains(ev.target as Node)) ev.preventDefault(); }}
      onKeyDown={(ev) => {
        if (ev.key === "Tab") {
          ev.preventDefault();
          close.current?.focus();
        }
      }}>
      <button type="button" ref={close} className="image-preview-close" data-action="image.close"
        aria-label={t("imagePreview.closeImagePreview")} onClick={onClose}><StudioIcon name="close" /></button>
      <div className="image-preview-view" data-action="image.close" onClick={backdrop}>
        {failed ? <p role="status">{t("common.imageUnavailable")}</p>
          : <img src={src} alt={name} draggable={false} onError={() => setFailed(true)} />}
      </div>
      <div className="image-preview-name">{name}</div>
    </div>, document.body,
  );
}
