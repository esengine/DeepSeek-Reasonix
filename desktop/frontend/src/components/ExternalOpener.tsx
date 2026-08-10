import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Check, ChevronDown, Code2, Folder, SquareTerminal } from "lucide-react";

import { app as desktopApp } from "../lib/bridge";
import { t } from "../lib/i18n";
import { useToast } from "../lib/toast";
import type { ExternalOpenerView, ExternalOpenersView, TabMeta } from "../lib/types";
import { Tooltip } from "./Tooltip";

export interface ExternalOpenerBridge {
  ExternalOpenersForTab(tabID: string): Promise<ExternalOpenersView>;
  SetPreferredExternalOpener(id: string): Promise<void>;
  OpenWorkspaceInExternalOpenerForTab(tabID: string, id: string): Promise<void>;
}

type ExternalOpenerPreferenceCoordinator = {
  nextIntent: number;
  latestSuccessfulIntent: number;
  writeTail: Promise<void>;
  preferred: string;
  revision: number;
  listeners: Set<(preferred: string) => void>;
};

const externalOpenerPreferenceCoordinators = new WeakMap<ExternalOpenerBridge, ExternalOpenerPreferenceCoordinator>();

function preferenceCoordinatorFor(bridge: ExternalOpenerBridge): ExternalOpenerPreferenceCoordinator {
  const current = externalOpenerPreferenceCoordinators.get(bridge);
  if (current) return current;
  const created: ExternalOpenerPreferenceCoordinator = {
    nextIntent: 0,
    latestSuccessfulIntent: 0,
    writeTail: Promise.resolve(),
    preferred: "",
    revision: 0,
    listeners: new Set(),
  };
  externalOpenerPreferenceCoordinators.set(bridge, created);
  return created;
}

function persistPreferredExternalOpener(
  bridge: ExternalOpenerBridge,
  id: string,
  coordinator: ExternalOpenerPreferenceCoordinator,
  intent: number,
): Promise<boolean> {
  const write = coordinator.writeTail.then(async () => {
    if (intent !== coordinator.latestSuccessfulIntent) return false;
    await bridge.SetPreferredExternalOpener(id);
    coordinator.preferred = id;
    coordinator.revision += 1;
    for (const listener of coordinator.listeners) listener(id);
    return true;
  });
  coordinator.writeTail = write.then(() => undefined, () => undefined);
  return write;
}

function fallbackOpenerIcon(opener: ExternalOpenerView) {
  if (opener.kind === "file-manager") return <Folder size={15} strokeWidth={1.9} />;
  if (opener.kind === "terminal") return <SquareTerminal size={15} strokeWidth={1.9} />;
  return <Code2 size={15} strokeWidth={1.9} />;
}

function OpenerIcon({ opener }: { opener: ExternalOpenerView }) {
  return (
    <span className={`external-opener__app-icon external-opener__app-icon--${opener.kind}`} aria-hidden="true">
      <span className="external-opener__fallback-icon">{fallbackOpenerIcon(opener)}</span>
      {opener.iconDataUrl && (
        <img
          src={opener.iconDataUrl}
          alt=""
          draggable={false}
          onError={(event) => {
            event.currentTarget.hidden = true;
          }}
        />
      )}
    </span>
  );
}

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function normalizeOpeners(next: ExternalOpenersView): ExternalOpenersView {
  return {
    openers: Array.isArray(next.openers) ? next.openers : [],
    preferred: next.preferred ?? "",
    workspaceOpenable: next.workspaceOpenable === true,
  };
}

type ResolvedExternalOpeners = ExternalOpenersView & { tabId: string };

export function shouldMountExternalOpener(
  tab: Pick<TabMeta, "id" | "scope"> | null | undefined,
  imDetailVisible: boolean,
): boolean {
  return !imDetailVisible && Boolean(tab?.id);
}

export function ExternalOpener({
  tabId,
  dismissSignal,
  bridge = desktopApp,
}: {
  tabId: string;
  dismissSignal: number;
  bridge?: ExternalOpenerBridge;
}) {
  const { showToast } = useToast();
  const rootRef = useRef<HTMLDivElement>(null);
  const discoveryRequestRef = useRef(0);
  const mountedRef = useRef(true);
  const busyRef = useRef(false);
  const [state, setState] = useState<ResolvedExternalOpeners>({
    openers: [],
    preferred: "",
    workspaceOpenable: false,
    tabId: "",
  });
  const [menuOpen, setMenuOpen] = useState(false);
  const [busy, setBusy] = useState(false);

  const refreshOpeners = useCallback(async () => {
    const request = ++discoveryRequestRef.current;
    const preferenceCoordinator = preferenceCoordinatorFor(bridge);
    const preferenceRevision = preferenceCoordinator.revision;
    try {
      const next = normalizeOpeners(await bridge.ExternalOpenersForTab(tabId));
      if (preferenceCoordinator.revision !== preferenceRevision && preferenceCoordinator.preferred) {
        next.preferred = preferenceCoordinator.preferred;
      }
      if (mountedRef.current && request === discoveryRequestRef.current) setState({ ...next, tabId });
    } catch (error) {
      if (mountedRef.current && request === discoveryRequestRef.current) {
        console.error("Failed to discover external openers", error);
      }
    }
  }, [bridge, tabId]);

  useEffect(() => {
    mountedRef.current = true;
    const preferenceCoordinator = preferenceCoordinatorFor(bridge);
    const syncPreferred = (preferred: string) => {
      if (mountedRef.current) setState((current) => ({ ...current, preferred }));
    };
    preferenceCoordinator.listeners.add(syncPreferred);
    void refreshOpeners();
    return () => {
      mountedRef.current = false;
      discoveryRequestRef.current += 1;
      preferenceCoordinator.listeners.delete(syncPreferred);
    };
  }, [bridge, refreshOpeners]);

  useEffect(() => setMenuOpen(false), [dismissSignal, tabId]);

  useEffect(() => {
    if (!menuOpen) return;
    const onPointerDown = (event: MouseEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setMenuOpen(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") setMenuOpen(false);
    };
    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [menuOpen]);

  const selected = useMemo(
    () => state.tabId === tabId
      ? state.openers.find((opener) => opener.id === state.preferred) ?? state.openers[0]
      : undefined,
    [state, tabId],
  );

  const openIn = useCallback(
    async (opener: ExternalOpenerView, persist: boolean) => {
      if (busyRef.current) return;
      const preferenceCoordinator = persist ? preferenceCoordinatorFor(bridge) : undefined;
      const preferenceIntent = preferenceCoordinator ? ++preferenceCoordinator.nextIntent : 0;
      busyRef.current = true;
      discoveryRequestRef.current += 1;
      setBusy(true);
      setMenuOpen(false);
      try {
        // Launch before persisting so a config-write failure cannot block the
        // launch and a failed launch never becomes the saved preference.
        try {
          await bridge.OpenWorkspaceInExternalOpenerForTab(tabId, opener.id);
        } catch (error) {
          showToast(t("externalOpener.failed", { name: opener.name, error: errorText(error) }), "error");
          return;
        }
        if (preferenceCoordinator) {
          preferenceCoordinator.latestSuccessfulIntent = Math.max(
            preferenceCoordinator.latestSuccessfulIntent,
            preferenceIntent,
          );
          try {
            const persisted = await persistPreferredExternalOpener(
              bridge,
              opener.id,
              preferenceCoordinator,
              preferenceIntent,
            );
            if (persisted && mountedRef.current) setState((current) => ({ ...current, preferred: opener.id }));
          } catch (error) {
            showToast(t("externalOpener.persistFailed", { name: opener.name, error: errorText(error) }), "error");
          }
        }
      } finally {
        busyRef.current = false;
        setBusy(false);
      }
    },
    [bridge, showToast, tabId],
  );

  if (state.tabId !== tabId || state.workspaceOpenable !== true || !selected) return null;
  const openLabel = t("externalOpener.openIn", { name: selected.name });

  return (
    <div ref={rootRef} className={`external-opener${menuOpen ? " external-opener--open" : ""}`}>
      <Tooltip label={openLabel} className="external-opener__primary-wrap">
        <button
          className="external-opener__primary"
          type="button"
          disabled={busy}
          aria-label={openLabel}
          onClick={() => void openIn(selected, false)}
        >
          <OpenerIcon opener={selected} />
        </button>
      </Tooltip>
      <button
        className="external-opener__menu-trigger"
        type="button"
        disabled={busy}
        aria-label={t("externalOpener.choose")}
        title={t("externalOpener.choose")}
        aria-haspopup="menu"
        aria-expanded={menuOpen}
        onClick={() => {
          setMenuOpen((open) => !open);
          if (!menuOpen) void refreshOpeners();
        }}
      >
        <ChevronDown size={14} />
      </button>
      {menuOpen && (
        <div className="external-opener__menu" role="menu" aria-label={t("externalOpener.choose")}>
          {state.openers.map((opener) => (
            <button
              key={opener.id}
              type="button"
              role="menuitemradio"
              aria-checked={opener.id === selected.id}
              onClick={() => void openIn(opener, true)}
            >
              <OpenerIcon opener={opener} />
              <span>{opener.name}</span>
              {opener.id === selected.id && <Check className="external-opener__check" size={15} aria-hidden="true" />}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
