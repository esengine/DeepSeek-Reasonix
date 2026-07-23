// Non-interactive full-window background layer for theme packs.
// Home scene uses full focus + opacity; task scene dims and applies a
// directional safe-area overlay (no backdrop-filter).

export function ThemeBackground() {
  return (
    <div className="theme-bg" aria-hidden="true">
      <div className="theme-bg__image" />
      <div className="theme-bg__overlay" />
    </div>
  );
}
