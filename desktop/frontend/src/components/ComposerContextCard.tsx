import type { ReactNode } from "react";
import { FileText, Folder, MessageSquare, X } from "lucide-react";
import { Tooltip } from "./Tooltip";

type ComposerContextCardVariant = "attachment" | "workspace" | "session";

export function ComposerContextCard({
  variant,
  tooltipLabel,
  removeLabel,
  onRemove,
  removeDisabled = false,
  removeIconSize,
  previewUrl,
  imageOnly = false,
  folder = false,
  name,
  meta,
  label,
  icon,
}: {
  variant: ComposerContextCardVariant;
  tooltipLabel: string;
  removeLabel: string;
  onRemove: () => void;
  removeDisabled?: boolean;
  removeIconSize?: number;
  previewUrl?: string;
  imageOnly?: boolean;
  folder?: boolean;
  name?: string;
  meta?: string;
  label?: ReactNode;
  icon?: ReactNode;
}) {
  const variantClass = previewUrl
    ? "composer-context__item--image"
    : variant === "workspace"
      ? `composer-context__item--workspace composer-context__item--${folder ? "folder" : "file"}`
      : variant === "session"
        ? "composer-context__item--session"
        : "composer-context__item--attachment";
  const iconNode = icon ?? (variant === "session" ? <MessageSquare size={15} /> : folder ? <Folder size={15} /> : <FileText size={15} />);
  return (
    <div className={`composer-context__item ${variantClass}${imageOnly ? " composer-context__item--image-only" : ""}`}>
      <Tooltip label={tooltipLabel}>
        <span className="composer-context__label">
          {previewUrl ? (
            <span className="composer-context__thumb">
              <img src={previewUrl} alt="" draggable={false} />
            </span>
          ) : variant === "attachment" ? (
            <>
              <span className="composer-context__fileicon">
                {icon ?? <FileText size={20} />}
              </span>
              <span className="composer-context__main">
                <span className="composer-context__name">{name}</span>
                {meta && <span className="composer-context__meta">{meta}</span>}
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
        <button className="composer-context__remove" type="button" disabled={removeDisabled} onClick={onRemove}>
          <X size={removeIconSize ?? (variant === "attachment" ? 14 : 13)} />
        </button>
      </Tooltip>
    </div>
  );
}
