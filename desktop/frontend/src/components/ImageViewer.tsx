import { useCallback, useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { X } from "lucide-react";
import { useT } from "../lib/i18n";

export interface ImageViewerProps {
  open: boolean;
  /** data-URL of the image to display */
  imageUrl: string;
  /** optional filename shown at the bottom */
  imageName?: string;
  onClose: () => void;
}

/**
 * ImageViewer renders a full-size image preview in a portal overlay.
 *
 * It follows the same portal pattern as MermaidDiagram fullscreen:
 * the overlay is portaled into .chat-pane (fallback document.body) so it
 * covers the chat area without being clipped by overflow containers.
 */
export function ImageViewer({ open, imageUrl, imageName, onClose }: ImageViewerProps) {
  const t = useT();
  const [portalTarget, setPortalTarget] = useState<Element | null>(null);
  const [visible, setVisible] = useState(false);

  // Resolve portal target when opening.
  useEffect(() => {
    if (!open) {
      setVisible(false);
      setPortalTarget(null);
      return;
    }
    const target = document.querySelector(".chat-pane") ?? document.body;
    setPortalTarget(target);
    // Trigger enter animation on the next frame.
    const raf = requestAnimationFrame(() => setVisible(true));
    return () => cancelAnimationFrame(raf);
  }, [open]);

  // Close on Escape.
  useEffect(() => {
    if (!open) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [open, onClose]);

  // Prevent body scroll while open.
  useEffect(() => {
    if (!open) return;
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = prev;
    };
  }, [open]);

  const handleBackdropClick = useCallback(
    (e: React.MouseEvent) => {
      if (e.target === e.currentTarget) onClose();
    },
    [onClose],
  );

  if (!open || !portalTarget || !imageUrl) return null;

  const overlay = (
    <div
      className={`image-viewer-backdrop${visible ? " image-viewer--enter" : ""}`}
      onClick={handleBackdropClick}
      role="dialog"
      aria-modal="true"
      aria-label={imageName || t("imageViewer.title")}
    >
      <button
        className="image-viewer__close"
        type="button"
        onClick={onClose}
        aria-label={t("imageViewer.closePreview")}
      >
        <X size={22} />
      </button>
      <div className="image-viewer__content">
        <img
          className="image-viewer__image"
          src={imageUrl}
          alt={imageName || ""}
          draggable={false}
        />
        {imageName && <div className="image-viewer__name">{imageName}</div>}
      </div>
    </div>
  );

  return createPortal(overlay, portalTarget);
}
