import { Component, Suspense, lazy, useState, type ComponentType, type ReactNode } from "react";
import { useT } from "../lib/i18n";
class SurfaceBoundary extends Component<{ fallback: ReactNode; children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  render() { return this.state.failed ? this.props.fallback : this.props.children; }
}
/** Recreate the lazy loader on retry; a rejected React.lazy promise is cached. */
export function ManagementSurface<P extends object>({ loader, surfaceProps, active, onBack }: {
  loader: () => Promise<{ default: ComponentType<P> }>; surfaceProps: P; active: boolean; onBack: () => void;
}) {
  const t = useT();
  const [attempt, setAttempt] = useState(() => ({ key: 0, View: lazy(loader) }));
  const { View } = attempt;
  const fallback = (failed: boolean) => active ? <section aria-label={t("settings.title")} style={{ position: "fixed", inset: 0, zIndex: "var(--z-modal)", background: "var(--bg-soft)", padding: "60px 24px", color: "var(--fg)" }}>
    <button className="btn" onClick={onBack}>{t("settings.backToWorkspace")}</button>
    <p role={failed ? "alert" : "status"}>{t(failed ? "settings.loadFailed" : "common.loading")}</p>
    {failed && <button className="btn" onClick={() => setAttempt((value) => ({ key: value.key + 1, View: lazy(loader) }))}>{t("common.retry")}</button>}
  </section> : null;
  return <SurfaceBoundary key={attempt.key} fallback={fallback(true)}><Suspense fallback={fallback(false)}><View {...surfaceProps} /></Suspense></SurfaceBoundary>;
}
