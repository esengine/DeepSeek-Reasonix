import { asArray } from "./array";
import { app } from "./bridge";
import { startDesktopEventRecovery } from "./desktopEventRecovery";
import { recordFrontendDiagnostic } from "./frontendDiagnosticBridge";
import type { Meta, TabMeta } from "./types";

export interface ControllerRecoveryPorts {
  navigation(): number;
  bindings(): Map<string, string>;
  meta(tabId: string): Meta | undefined;
  now(): number;
  flush(): void;
  prepare(tab: TabMeta): void;
  runtime(tab: TabMeta, snapshotAt: number): void;
  reset(tabId: string): void;
  hydrate(tab: TabMeta, isCurrent: () => boolean): Promise<void>;
}

export function startControllerEventRecovery(ports: ControllerRecoveryPorts, subscribe: (callback: () => void) => () => void): () => void {
  let version = 0;
  const hydrating = new Set<string>();
  const failed = (error: unknown) => recordFrontendDiagnostic("runtime", "desktop-event-recovery-failed", {
    error: error instanceof Error ? error.message : String(error),
  });
  const off = startDesktopEventRecovery({
    subscribe: callback => subscribe(() => { version++; callback(); }),
    capture: () => ({ version, navigation: ports.navigation(), snapshotAt: ports.now(), bindings: ports.bindings() }),
    isCurrent: scope => {
      const bindings = ports.bindings();
      return scope.navigation === ports.navigation() && Array.from(scope.bindings).every(([id, value]) => bindings.get(id) === value);
    },
    read: async () => asArray(await app.ListTabs()),
    apply: (tabs, scope) => {
      ports.flush();
      for (const tab of tabs) {
        if (!scope.bindings.has(tab.id)) continue;
        const meta = ports.meta(tab.id);
        const changedSession = meta?.sessionPath !== tab.sessionPath || meta?.sessionGeneration !== tab.sessionGeneration;
        ports.prepare(tab);
        if (changedSession || hydrating.has(tab.id)) {
          hydrating.add(tab.id);
          ports.reset(tab.id);
          const isCurrent = () => scope.version === version && scope.navigation === ports.navigation();
          const hydration = ports.hydrate(tab, isCurrent);
          // hydrate advances sessionLoadSeq synchronously before its first
          // await. Capture that new binding, not the pre-hydration snapshot:
          // a background session change can supersede it without navigation.
          const binding = ports.bindings().get(tab.id);
          void hydration.then(() => {
            if (isCurrent() && binding !== undefined && ports.bindings().get(tab.id) === binding) {
              hydrating.delete(tab.id); ports.runtime(tab, scope.snapshotAt);
            }
          }).catch(failed);
        } else {
          // The existing runtime projection requests missing durable turn
          // events and pending prompt presentation, never another invocation.
          ports.runtime(tab, scope.snapshotAt);
        }
      }
    },
    failed,
    timer: (callback, delay) => window.setTimeout(callback, delay),
    clearTimer: timer => { if (timer !== undefined) window.clearTimeout(timer as number); },
  });
  return () => { version++; off(); };
}
