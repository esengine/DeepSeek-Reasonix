// Prove both the renderer and the Wails bridge are alive. The native recovery
// coordinator requires a post-reload report within ten seconds.
export function installDesktopWebViewHeartbeat(): void {
  const reportReady = () => {
    const bindings = window.go?.main?.App;
    const report = bindings?.ReportDesktopWebViewReady;
    if (typeof report === "function") void Promise.resolve(report.call(bindings)).catch(() => undefined);
  };
  requestAnimationFrame(reportReady);
  window.setInterval(reportReady, 3_000);
  window.addEventListener("pageshow", reportReady);
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible") reportReady();
  });
}
