import type { ControllerRecoveryPorts } from "./controllerEventRecoveryWorker";
import { desktopHost } from "./desktopHost";
import { recordFrontendDiagnostic } from "./frontendDiagnosticBridge";

export type { ControllerRecoveryPorts } from "./controllerEventRecoveryWorker";

/** Listen immediately; recovery-only machinery loads after the first signal. */
export function startControllerEventRecovery(ports: ControllerRecoveryPorts): () => void {
  let disposed = false, loading = false, failures = 0;
  let recover: (() => void) | undefined, stop: (() => void) | undefined;
  let retry: ReturnType<typeof setTimeout> | undefined;
  const request = () => {
    if (disposed) return;
    if (recover) { recover(); return; }
    if (loading || retry !== undefined) return;
    loading = true;
    void import("./controllerEventRecoveryWorker").then(worker => {
      if (disposed) return;
      stop = worker.startControllerEventRecovery(ports, callback => {
        recover = callback;
        // All signals received during loading are covered by this fresh read.
        callback();
        return () => { recover = undefined; };
      });
    }).catch((error: unknown) => {
      if (disposed) return;
      loading = false;
      recordFrontendDiagnostic("runtime", "desktop-event-recovery-load-failed", { error: String(error) });
      retry = setTimeout(() => { retry = undefined; request(); }, Math.min(30000, 1000 * 2 ** Math.min(failures++, 5)));
    });
  };
  const off = desktopHost().events.on("desktop:resync", request);
  return () => { disposed = true; clearTimeout(retry); off(); stop?.(); };
}
