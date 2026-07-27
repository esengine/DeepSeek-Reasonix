import { createContext, createElement, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import { app, onUpdaterProgress } from "./bridge";
import type { UpdateInfo } from "./types";

// useUpdater drives the auto-update state machine shared by the top banner and the
// Settings panel: check, download/verify, then a separate restart/install action.
// Deb installs add an "authorizing" phase (Polkit) before the package manager runs.

export type UpdateStatus =
  | { kind: "idle" }
  | { kind: "checking" }
  | { kind: "upToDate"; current: string }
  | { kind: "available"; info: UpdateInfo }
  | { kind: "downloading"; received: number; total: number; info: UpdateInfo }
  | { kind: "verifying"; info: UpdateInfo }
  | { kind: "downloaded"; info: UpdateInfo }
  | { kind: "authorizing"; info?: UpdateInfo }
  | { kind: "installing"; info?: UpdateInfo }
  | { kind: "done" }
  | { kind: "error"; message: string; info?: UpdateInfo; manualHint?: boolean };

export interface Updater {
  status: UpdateStatus;
  check: (channel?: string) => Promise<void>;
  download: (info: UpdateInfo) => void;
  install: () => void;
  openDownload: () => void;
  reset: () => void;
}

function errMsg(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

function offersManualFallback(message: string): boolean {
  const low = message.toLowerCase();
  return (
    low.includes("authorization failed") ||
    low.includes("manual update required") ||
    low.includes("pkexec") ||
    low.includes("sudo apt install")
  );
}

const UpdaterContext = createContext<Updater | null>(null);

function useUpdaterInternal(): Updater {
  const [status, setStatus] = useState<UpdateStatus>({ kind: "idle" });

  // A single long-lived subscription advances the state machine through the apply
  // phases. It reads the in-flight info from the current state so progress events
  // carry the version/notes forward without re-plumbing them.
  useEffect(() => {
    return onUpdaterProgress((p) => {
      setStatus((cur) => {
        const info = "info" in cur ? cur.info : undefined;
        switch (p.phase) {
          case "downloading":
            return info ? { kind: "downloading", received: p.received, total: p.total, info } : cur;
          case "verifying":
            return info ? { kind: "verifying", info } : cur;
          case "downloaded":
            // Also used when the user cancels Polkit authorization so the UI
            // returns to "downloaded" and the install button can be clicked again.
            return info ? { kind: "downloaded", info: { ...info, downloaded: true } } : cur;
          case "authorizing":
            return { kind: "authorizing", info };
          case "installing":
            return { kind: "installing", info };
          case "done":
            return { kind: "done" };
          case "error":
            return {
              kind: "error",
              message: p.err ?? "update failed",
              info,
              manualHint: offersManualFallback(p.err ?? ""),
            };
          default:
            return cur;
        }
      });
    });
  }, []);

  const check = useCallback(async (channel = "") => {
    setStatus({ kind: "checking" });
    try {
      const info = await app.CheckUpdate(channel);
      if (!info) {
        setStatus({ kind: "upToDate", current: "" });
        return;
      }
      if (info.err) {
        setStatus({ kind: "error", message: info.err, info });
        return;
      }
      if (!info.available) {
        setStatus({ kind: "upToDate", current: info.current });
        return;
      }
      setStatus(info.downloaded ? { kind: "downloaded", info } : { kind: "available", info });
    } catch (e) {
      setStatus({ kind: "error", message: errMsg(e) });
    }
  }, []);

  const download = useCallback((info: UpdateInfo) => {
    if (!info.canSelfUpdate) {
      void app.OpenDownloadPage();
      return;
    }
    setStatus({ kind: "downloading", received: 0, total: info.assetSize, info });
    void app.DownloadUpdate(info.channel || "")
      .then((result) => {
        if (result) setStatus({ kind: "downloaded", info: { ...info, downloaded: true } });
      })
      .catch((e) => setStatus({ kind: "error", message: errMsg(e), info }));
  }, []);

  const install = useCallback(() => {
    setStatus((cur) => {
      const info = "info" in cur ? cur.info : undefined;
      // Deb installs start in authorizing; portable/other go straight to installing.
      if (info?.requiresElevation || info?.installMode === "deb") {
        return { kind: "authorizing", info };
      }
      return { kind: "installing", info };
    });
    void app.InstallUpdate("info" in status ? status.info?.channel || "" : "").catch((e) => {
      const message = errMsg(e);
      setStatus((cur) => {
        const info = "info" in cur ? cur.info : undefined;
        return { kind: "error", message, info, manualHint: offersManualFallback(message) };
      });
    });
  }, [status]);

  const openDownload = useCallback(() => {
    void app.OpenDownloadPage();
  }, []);

  const reset = useCallback(() => setStatus({ kind: "idle" }), []);

  return { status, check, download, install, openDownload, reset };
}

export function UpdaterProvider({ children }: { children: ReactNode }) {
  const updater = useUpdaterInternal();
  return createElement(UpdaterContext.Provider, { value: updater, children });
}

export function useUpdater(): Updater {
  const updater = useContext(UpdaterContext);
  if (!updater) throw new Error("useUpdater must be used within an UpdaterProvider");
  return updater;
}
