/** Tiny hot-path bridge. The full recorder is loaded only by test builds. */
export type FrontendDiagnosticFields = Record<string, unknown>;
type DiagnosticSink = (source: string, type: string, fields: FrontendDiagnosticFields) => void;
type DiagnosticStartHook = () => void;

let sink: DiagnosticSink | undefined;
const startHooks = new Set<DiagnosticStartHook>();

export function setFrontendDiagnosticSink(next: DiagnosticSink): void {
  sink = next;
}

export function recordFrontendDiagnostic(source: string, type: string, fields: FrontendDiagnosticFields = {}): void {
  sink?.(source, type, fields);
}

/** Register a lightweight producer that should publish its initial snapshot
 * whenever the opt-in recorder starts. The bridge stays inert when no recorder
 * is loaded, so stable builds pay only for this small Set and effect cleanup. */
export function registerFrontendDiagnosticStartHook(hook: DiagnosticStartHook): () => void {
  startHooks.add(hook);
  return () => startHooks.delete(hook);
}

export function notifyFrontendDiagnosticStart(): void {
  for (const hook of startHooks) {
    try {
      hook();
    } catch {
      // A diagnostic producer must never prevent the recorder from starting.
    }
  }
}
