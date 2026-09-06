import { useEffect, useState } from "react";
import { t } from "../i18n";
import { host } from "../port/host";

// Where the chrome is standing in for the title bar. Linux keeps its own frame,
// and a browser tab has no window to zoom.
const isTitleBar = () => {
  const d = document.documentElement.dataset;
  return d.titlebar === "app" && (d.platform === "darwin" || d.platform === "windows");
};

// Double-clicking a title bar zooms the window on both platforms. The drag
// region is drawn by the webview, so nothing native is left to hear the click.
export function zoomOnTitleBar(e: { target: EventTarget | null }) {
  if (!isTitleBar()) return;
  const el = e.target as HTMLElement | null;
  if (el?.closest("button, input, textarea, .picker, .menu")) return;
  host().toggleMaximiseWindow();
}

// Frameless Windows has no native minimise/maximise/close, so a shell that goes
// frameless without drawing these leaves a window that can only be closed from
// the keyboard. Rendered on Windows alone: macOS keeps its own lights and Linux
// its whole frame.
export function WindowControls() {
  const [max, setMax] = useState(false);
  // The window owns this state, so read it back rather than inferring it from
  // our own click: the button is one of several ways it changes, alongside a
  // double-click on the bar, a snap and the keyboard.
  useEffect(() => {
    const read = () => {
      void host().isWindowMaximised().then(setMax);
    };
    read();
    // The observer the layout already rides on, so it is the one proven to fire
    // in this shell when the window changes size.
    const ro = new ResizeObserver(read);
    ro.observe(document.body);
    return () => ro.disconnect();
  }, []);
  if (document.documentElement.dataset.platform !== "windows") return null;
  return (
    <div className="winctl" role="group" aria-label={t("窗口")}>
      <button className="wc" data-action="window.minimize" onClick={() => host().minimiseWindow()} aria-label={t("最小化")}>
        <svg viewBox="0 0 12 12" aria-hidden="true">
          <path d="M2 6h8" />
        </svg>
      </button>
      <button
        className="wc"
        data-action="window.maximize"
        onClick={() => host().toggleMaximiseWindow()}
        aria-label={t(max ? "还原" : "最大化")}
      >
        <svg viewBox="0 0 12 12" aria-hidden="true">
          {max ? (
            <>
              <path d="M3.4 3.4V2.2h6.4v6.4H8.6" />
              <path d="M2.2 3.4h6.4v6.4H2.2Z" />
            </>
          ) : (
            <path d="M2.4 2.4h7.2v7.2H2.4Z" />
          )}
        </svg>
      </button>
      <button className="wc close" data-action="window.close" onClick={() => host().closeWindow()} aria-label={t("关闭")}>
        <svg viewBox="0 0 12 12" aria-hidden="true">
          <path d="M2.6 2.6l6.8 6.8M9.4 2.6l-6.8 6.8" />
        </svg>
      </button>
    </div>
  );
}
