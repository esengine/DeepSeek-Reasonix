import { lazy, Suspense, type ComponentProps, type KeyboardEvent, type PointerEvent } from "react";
import { StatusBar } from "../components/StatusBar";
import type { Translator } from "../lib/i18n";

const TerminalPanel = lazy(() => import("../components/TerminalPanel").then((module) => ({ default: module.TerminalPanel })));

type AppBottomRegionsProps = {
  terminal: {
    surfaceVisible?: boolean;
    open: boolean;
    contentVisible: boolean;
    remoteSurface: boolean;
    t: Translator;
    panel: ComponentProps<typeof TerminalPanel>;
    resizer: {
      min: number;
      max: number;
      value: number;
      onPointerDown: (event: PointerEvent<HTMLButtonElement>) => void;
      onKeyDown: (event: KeyboardEvent<HTMLButtonElement>) => void;
      onReset: () => void;
    };
  };
  status?: ComponentProps<typeof StatusBar>;
};

/** Bottom shell surfaces remain mounted according to their original lifecycle. */
export function AppBottomRegions({ terminal, status }: AppBottomRegionsProps) {
  return (
    <>
      <aside className="terminal-drawer" aria-label={terminal.t("terminal.title")} aria-hidden={!terminal.open} inert={!terminal.open ? true : undefined}>
        {!terminal.remoteSurface && terminal.contentVisible && (
          <Suspense fallback={<div className="terminal-empty"><span className="terminal-empty__spinner" />{terminal.t("terminal.loading")}</div>}>
            <TerminalPanel {...terminal.panel} />
          </Suspense>
        )}
      </aside>
      {terminal.surfaceVisible !== false && <button
        className="terminal-drawer-resizer" type="button" role="separator" aria-orientation="horizontal"
        aria-label={terminal.t("terminal.resize")} aria-valuemin={terminal.resizer.min}
        aria-valuemax={terminal.resizer.max} aria-valuenow={terminal.resizer.value}
        aria-hidden={!terminal.open} tabIndex={terminal.open ? 0 : -1}
        onPointerDown={terminal.resizer.onPointerDown} onKeyDown={terminal.resizer.onKeyDown}
        onDoubleClick={terminal.resizer.onReset}
      />}
      {status && <StatusBar {...status} />}
    </>
  );
}
