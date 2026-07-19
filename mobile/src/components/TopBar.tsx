import type { ReactNode } from "react";

export function TopBar({
  title,
  subtitle,
  leading,
  trailing,
  largeTitle,
}: {
  title: string;
  subtitle?: string;
  leading?: ReactNode;
  trailing?: ReactNode;
  /** When true, title is visually secondary (iOS large title page uses empty center). */
  largeTitle?: boolean;
}) {
  return (
    <header className="top-bar" role="banner">
      <div className="top-bar-side">{leading}</div>
      <div className="top-bar-title-block">
        {!largeTitle && (
          <>
            <div className="top-bar-title">{title}</div>
            {subtitle ? <div className="top-bar-subtitle">{subtitle}</div> : null}
          </>
        )}
        {largeTitle && subtitle ? (
          <div className="top-bar-subtitle">{subtitle}</div>
        ) : null}
      </div>
      <div className="top-bar-side end">{trailing}</div>
    </header>
  );
}
