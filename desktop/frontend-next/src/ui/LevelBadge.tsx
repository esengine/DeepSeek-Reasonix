import type { ReactNode } from "react";

const GROUND = <path d="M4.5 20.5h15" />;

function petal(angle: number): ReactNode {
  return <ellipse key={angle} cx="12" cy="4.5" rx="2.1" ry="2.7" transform={`rotate(${angle} 12 9.5)`} fill="currentColor" stroke="none" />;
}

export const LEVEL_GLYPHS: readonly ReactNode[] = [
  <>
    <ellipse cx="12" cy="13.2" rx="3.9" ry="5.9" transform="rotate(24 12 13.2)" fill="currentColor" stroke="none" />
    {GROUND}
  </>,
  <>
    <path d="M12 20.5V11" />
    <path d="M12 11c0-4 3-6.5 7-6.5 0 4-3 6.5-7 6.5z" fill="currentColor" />
    {GROUND}
  </>,
  <>
    <path d="M12 20.5V9" />
    <path d="M12 13.2c0-3.6 2.5-6.2 6.7-6.2 0 3.6-2.5 6.2-6.7 6.2z" fill="currentColor" />
    <path d="M12 11.6c0-3.2-2.3-5.6-6-5.6 0 3.2 2.3 5.6 6 5.6z" fill="currentColor" />
    {GROUND}
  </>,
  <>
    <path d="M12 20.5V10.5" />
    <circle cx="8.4" cy="9.4" r="3.5" fill="currentColor" stroke="none" />
    <circle cx="15.6" cy="9.4" r="3.5" fill="currentColor" stroke="none" />
    <circle cx="12" cy="5.8" r="3.5" fill="currentColor" stroke="none" />
    {GROUND}
  </>,
  <>
    {[0, 72, 144, 216, 288].map(petal)}
    <circle cx="12" cy="9.5" r="1.5" fill="currentColor" stroke="none" />
    <path d="M12 17.4v3.1" />
    {GROUND}
  </>,
  <>
    <circle cx="6.8" cy="10.8" r="4.4" fill="currentColor" stroke="none" />
    <circle cx="17.2" cy="10.8" r="4.4" fill="currentColor" stroke="none" />
    <circle cx="12" cy="7.2" r="5.2" fill="currentColor" stroke="none" />
    <path d="M12 21V12.5" strokeWidth="2.2" />
  </>,
  <>
    <path d="M12 3l5 10H7z" fill="currentColor" />
    <path d="M5 8.8l3.4 6.8H1.6z" fill="currentColor" />
    <path d="M19 8.8l3.4 6.8h-6.8z" fill="currentColor" />
    <path d="M12 13v7.5M5 15.6v4.9M19 15.6v4.9" />
    {GROUND}
  </>,
];

const LOCKED = (
  <>
    <circle cx="12" cy="12" r="9.4" strokeDasharray="2.3 2.7" />
    <rect x="8" y="11.2" width="8" height="6.2" rx="1.4" fill="currentColor" stroke="none" />
    <path d="M9.8 11.2V9.4a2.2 2.2 0 0 1 4.4 0v1.8" />
  </>
);

interface Props {
  level: number;
  locked?: boolean;
  size?: number;
  // Set where the level's name is printed beside the badge; otherwise label names it.
  decorative?: boolean;
  label?: string;
}

export function LevelBadge({ level, locked = false, size = 24, decorative = false, label }: Props) {
  const glyph = LEVEL_GLYPHS[Math.min(Math.max(Number.isInteger(level) ? level : 0, 0), LEVEL_GLYPHS.length - 1)];
  const named = !decorative && label !== undefined;
  return (
    <svg
      className="lvl-badge"
      viewBox="0 0 24 24"
      width={size}
      height={size}
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      data-level={level}
      data-locked={locked ? "" : undefined}
      role={named ? "img" : undefined}
      aria-label={named ? label : undefined}
      aria-hidden={named ? undefined : true}
      focusable="false"
    >
      {locked ? LOCKED : glyph}
    </svg>
  );
}
