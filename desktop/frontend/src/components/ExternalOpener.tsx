import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Check, ChevronDown, Code2, Folder, SquareTerminal } from "lucide-react";

import { app as desktopApp } from "../lib/bridge";
import { t } from "../lib/i18n";
import { useToast } from "../lib/toast";
import type { ExternalOpenerView, ExternalOpenersView } from "../lib/types";
import { Tooltip } from "./Tooltip";

export interface ExternalOpenerBridge {
  ExternalOpeners(): Promise<ExternalOpenersView>;
  SetPreferredExternalOpener(id: string): Promise<void>;
  OpenWorkspaceInExternalOpenerForTab(tabID: string, id: string): Promise<void>;
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
  return { openers: Array.isArray(next.openers) ? next.openers : [], preferred: next.preferred ?? "" };
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
  const [state, setState] = useState<ExternalOpenersView>({ openers: [], preferred: "" });
  const [menuOpen, setMenuOpen] = useState(false);
  const [busy, setBusy] = useState(false);

  const refreshOpeners = useCallback(async () => {
    const request = ++discoveryRequestRef.current;
    try {
      const next = normalizeOpeners(await bridge.ExternalOpeners());
      if (mountedRef.current && request === discoveryRequestRef.current) setState(next);
    } catch (error) {
      if (mountedRef.current && request === discoveryRequestRef.current) {
        console.error("Failed to discover external openers", error);
      }
    }
  }, [bridge]);

  useEffect(() => {
    mountedRef.current = true;
    void refreshOpeners();
    return () => {
      mountedRef.current = false;
      discoveryRequestRef.current += 1;
    };
  }, [refreshOpeners]);

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
    () => state.openers.find((opener) => opener.id === state.preferred) ?? state.openers[0],
    [state],
  );

  const openIn = useCallback(
    async (opener: ExternalOpenerView, persist: boolean) => {
      if (busyRef.current) return;
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
        if (persist && opener.id !== state.preferred) {
          try {
            await bridge.SetPreferredExternalOpener(opener.id);
            setState((current) => ({ ...current, preferred: opener.id }));
          } catch (error) {
            showToast(t("externalOpener.persistFailed", { name: opener.name, error: errorText(error) }), "error");
          }
        }
      } finally {
        busyRef.current = false;
        setBusy(false);
      }
    },
    [bridge, showToast, state.preferred, tabId],
  );

  if (!selected) return null;
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
