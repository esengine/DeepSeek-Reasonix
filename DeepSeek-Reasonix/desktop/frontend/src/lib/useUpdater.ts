import { useCallback, useEffect, useState } from "react";
import { app, onUpdaterProgress } from "./bridge";
import type { UpdateInfo } from "./types";

// useUpdater drives the auto-update state machine shared by the top banner (auto
// check on startup) and the Settings panel (manual check): a manifest check, then
// either an in-place apply (win/linux, streaming progress on the "updater:progress"
// event) or the macOS manual-download fallback. The progress subscription lives for
// the hook's lifetime so it unsubscribes on unmount.

export type UpdateStatus =
  | { kind: "idle" }
  | { kind: "checking" }
  | { kind: "upToDate"; current: string }
  | { kind: "available"; info: UpdateInfo }
  | { kind: "downloading"; received: number; total: number; info: UpdateInfo }
  | { kind: "verifying"; info: UpdateInfo }
  | { kind: "applying"; info: UpdateInfo }
  | { kind: "done" }
  | { kind: "error"; message: string };

export interface Updater {
  status: UpdateStatus;
  check: () => Promise<void>;
  apply: (info: UpdateInfo) => void;
  openDownload: () => void;
  reset: () => void;
}

function errMsg(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

export function useUpdater(): Updater {
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
          case "applying":
            return info ? { kind: "applying", info } : cur;
          case "done":
            return { kind: "done" };
          case "error":
            return { kind: "error", message: p.err ?? "update failed" };
          default:
            return cur;
        }
      });
    });
  }, []);

  const check = useCallback(async () => {
    setStatus({ kind: "checking" });
    try {
      const info = await app.CheckUpdate();
      if (!info) {
        setStatus({ kind: "upToDate", current: "" });
        return;
      }
      if (info.err) {
        setStatus({ kind: "error", message: info.err });
        return;
      }
      setStatus(info.available ? { kind: "available", info } : { kind: "upToDate", current: info.current });
    } catch (e) {
      setStatus({ kind: "error", message: errMsg(e) });
    }
  }, []);

  // apply takes the already-fetched info (rather than reading state) so there's no
  // side effect inside a state updater. macOS can't self-update → open the page.
  const apply = useCallback((info: UpdateInfo) => {
    if (!info.canSelfUpdate) {
      void app.OpenDownloadPage();
      return;
    }
    setStatus({ kind: "downloading", received: 0, total: info.assetSize, info });
    // On success the process exits/relaunches and this never resolves; a failure
    // surfaces here (the "updater:progress" error event also covers backend faults).
    void app.ApplyUpdate().catch((e) => setStatus({ kind: "error", message: errMsg(e) }));
  }, []);

  const openDownload = useCallback(() => {
    void app.OpenDownloadPage();
  }, []);

  const reset = useCallback(() => setStatus({ kind: "idle" }), []);

  return { status, check, apply, openDownload, reset };
}
