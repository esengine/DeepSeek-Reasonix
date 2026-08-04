import type { ReactNode } from "react";
import { FileText, Folder, MessageSquare, X } from "lucide-react";
import { Tooltip } from "./Tooltip";

type ComposerContextCardVariant = "attachment" | "workspace" | "session" | "selection";

export function ComposerContextCard({
  variant,
  tooltipLabel,
  removeLabel,
  onRemove,
  removeDisabled = false,
  removeIconSize,
  previewUrl,
  onImageClick,
  imageOnly = false,
  folder = false,
  name,
  meta,
  note,
  label,
  icon,
}: {
  variant: ComposerContextCardVariant;
  tooltipLabel: ReactNode;
  removeLabel: string;
  onRemove: () => void;
  removeDisabled?: boolean;
  removeIconSize?: number;
  previewUrl?: string;
  /** called when the image thumbnail is clicked (only for image attachments with previewUrl) */
  onImageClick?: () => void;
  imageOnly?: boolean;
  folder?: boolean;
  name?: string;
  meta?: string;
  /** optional one-line note (e.g. the user's comment on a selection) shown under meta */
  note?: string;
  label?: ReactNode;
  icon?: ReactNode;
}) {
  const variantClass = previewUrl
    ? "composer-context__item--image"
    : variant === "workspace"
      ? `composer-context__item--workspace composer-context__item--${folder ? "folder" : "file"}`
      : variant === "session"
        ? "composer-context__item--session"
        : variant === "selection"
          ? "composer-context__item--selection composer-context__item--attachment"
        : "composer-context__item--attachment";
  const iconNode = icon ?? (variant === "session" ? <MessageSquare size={15} /> : folder ? <Folder size={15} /> : <FileText size={15} />);
  return (
    <div className={`composer-context__item ${variantClass}${imageOnly ? " composer-context__item--image-only" : ""}`}>
      <Tooltip label={tooltipLabel}>
        <span className="composer-context__label">
          {previewUrl ? (
            <span
              className={`composer-context__thumb${onImageClick ? " composer-context__thumb--interactive" : ""}`}
              onClick={onImageClick}
              role={onImageClick ? "button" : undefined}
              tabIndex={onImageClick ? 0 : undefined}
              onKeyDown={onImageClick ? (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onImageClick(); } } : undefined}
            >
              <img src={previewUrl} alt="" draggable={false} />
            </span>
          ) : variant === "attachment" || variant === "selection" ? (
            <>
              <span className="composer-context__fileicon">
                {icon ?? <FileText size={20} />}
              </span>
              <span className="composer-context__main">
                <span className="composer-context__name">{name}</span>
                {meta && <span className="composer-context__meta">{meta}</span>}
                {note && <span className="composer-context__note">{note}</span>}
              </span>
            </>
          ) : (
            <>
              {iconNode}
              <span>{label}</span>
            </>
          )}
        </span>
      </Tooltip>
      <Tooltip label={removeLabel} className="composer-context__remove-trigger">
        <button
          className="composer-context__remove"
          type="button"
          aria-label={removeLabel}
          disabled={removeDisabled}
          onClick={onRemove}
        >
          <X size={removeIconSize ?? (variant === "attachment" ? 14 : 13)} aria-hidden="true" />
        </button>
      </Tooltip>
    </div>
  );
}
