import { Children, type ReactNode, type SVGProps, useState } from "react";

type IconProps = SVGProps<SVGSVGElement> & { size?: number };
export type ProcessTone = "default" | "success" | "warning" | "danger" | "accent" | "violet";
export type ProcessState = "running" | "done" | "failed" | "waiting" | "stopped";

function ProcessIcon({ size = 14, children, ...rest }: IconProps & { children: ReactNode }) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      {...rest}
    >
      {children}
    </svg>
  );
}

export function ProcessChevronIcon(props: IconProps) {
  return (
    <ProcessIcon {...props}>
      <path d="m6 9 6 6 6-6" />
    </ProcessIcon>
  );
}

export function ProcessCheckIcon(props: IconProps) {
  return (
    <ProcessIcon {...props}>
      <path d="m5 12 5 5L20 7" />
    </ProcessIcon>
  );
}

export function ProcessXIcon(props: IconProps) {
  return (
    <ProcessIcon {...props}>
      <path d="M6 6l12 12M18 6 6 18" />
    </ProcessIcon>
  );
}

export function ProcessBrainIcon(props: IconProps) {
  return (
    <ProcessIcon {...props}>
      <path d="M9 4a3 3 0 0 0-3 3v0a3 3 0 0 0-2 5 3 3 0 0 0 2 5 3 3 0 0 0 3 3h0a3 3 0 0 0 3-3V4" />
      <path d="M15 4a3 3 0 0 1 3 3 3 3 0 0 1 2 5 3 3 0 0 1-2 5 3 3 0 0 1-3 3" />
    </ProcessIcon>
  );
}

export function ProcessToolIcon(props: IconProps) {
  return (
    <ProcessIcon {...props}>
      <path d="M14 7a4 4 0 1 0 4 4l3 3-3 3-3-3a4 4 0 0 1-4-4l-3-3-3 3 3 3a4 4 0 0 0 6 0" />
    </ProcessIcon>
  );
}

export function ProcessInfoIcon(props: IconProps) {
  return (
    <ProcessIcon {...props}>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 16v-4" />
      <path d="M12 8h.01" />
    </ProcessIcon>
  );
}

export function ProcessPhaseIcon(props: IconProps) {
  return (
    <ProcessIcon {...props}>
      <path d="M4 7h9" />
      <path d="M4 12h13" />
      <path d="M4 17h7" />
      <path d="m17 7 3 3-3 3" />
    </ProcessIcon>
  );
}

export function ProcessCompactIcon(props: IconProps) {
  return (
    <ProcessIcon {...props}>
      <path d="M8 4h8" />
      <path d="M6 8h12" />
      <rect x="4" y="12" width="16" height="8" rx="2" />
      <path d="M8 16h8" />
    </ProcessIcon>
  );
}

export function ProcessStatusIcon({ state, label }: { state: ProcessState; label: string }) {
  if (state === "running") return <span className="process-card__spin" role="img" aria-label={label} title={label} />;
  if (state === "done") return <ProcessCheckIcon className="process-card__status process-card__status--done" size={12} aria-label={label} />;
  if (state === "failed") return <ProcessXIcon className="process-card__status process-card__status--failed" size={12} aria-label={label} />;
  return <span className={`process-card__dot process-card__dot--${state}`} role="img" aria-label={label} title={label} />;
}

export function ProcessCard({
  tone = "default",
  icon,
  kind,
  name,
  meta,
  defaultOpen = false,
  open,
  onOpenChange,
  children,
  className,
}: {
  tone?: ProcessTone;
  icon: ReactNode;
  kind: ReactNode;
  name?: ReactNode;
  meta?: ReactNode;
  defaultOpen?: boolean;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  children?: ReactNode;
  className?: string;
}) {
  const [internalOpen, setInternalOpen] = useState(defaultOpen);
  const actualOpen = open ?? internalOpen;
  const hasBody = Children.count(children) > 0;
  const toggle = () => {
    if (!hasBody) return;
    const next = !actualOpen;
    if (open === undefined) setInternalOpen(next);
    onOpenChange?.(next);
  };

  return (
    <div className={`process-card${className ? ` ${className}` : ""}`} data-tone={tone} data-open={actualOpen} data-has-body={hasBody}>
      <button
        type="button"
        className="process-card__head"
        onClick={toggle}
        onKeyDown={(e) => { if (e.key === "Escape") { e.preventDefault(); toggle(); } }}
        aria-expanded={hasBody ? actualOpen : undefined}
      >
        <span className="process-card__icon">{icon}</span>
        <span className="process-card__kind">{kind}</span>
        {name && <span className="process-card__name">{name}</span>}
        <span className="process-card__grow" />
        {meta && <span className="process-card__meta">{meta}</span>}
        {hasBody && (
          <span className="process-card__chevron">
            <ProcessChevronIcon size={12} />
          </span>
        )}
      </button>
      {hasBody && (
        <div className="process-card__wrap" aria-hidden={!actualOpen}>
          <div>
            <div className="process-card__body">{children}</div>
          </div>
        </div>
      )}
    </div>
  );
}
