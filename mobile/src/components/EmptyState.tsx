import type { ReactNode } from "react";

export function EmptyState({
  icon,
  title,
  description,
  actions,
}: {
  icon: ReactNode;
  title: string;
  description: string;
  actions?: ReactNode;
}) {
  return (
    <div className="empty-state">
      <div className="empty-icon" aria-hidden>
        {icon}
      </div>
      <h2 className="empty-title">{title}</h2>
      <p className="empty-desc">{description}</p>
      {actions ? <div className="empty-actions">{actions}</div> : null}
    </div>
  );
}
