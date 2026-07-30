import { createContext, createElement, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";
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
  reset: (channel?: "stable" | "preview") => void;
}

export async function switchUpdaterChannel(
  channel: "stable" | "preview",
  invalidate: (channel: "stable" | "preview") => void,
  save: (channel: "stable" | "preview") => Promise<boolean>,
  check: (channel: string) => Promise<void>,
): Promise<void> {
  invalidate(channel);
  if (await save(channel)) await check(channel);
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

type UpdaterOperationKind = "idle" | "checking" | "ready" | "downloading" | "installing";

interface UpdaterOperation {
  epoch: number;
  requestId: string;
  channel: "" | "stable" | "preview";
  expectedVersion: string;
  kind: UpdaterOperationKind;
}

let updaterRequestSequence = 0;
const updaterRequestPrefix = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;

function nextUpdaterRequestId(epoch: number): string {
  updaterRequestSequence += 1;
  return `web-${updaterRequestPrefix}-${epoch}-${updaterRequestSequence}`;
}

function normalizedChannel(channel: string): "stable" | "preview" {
  return channel === "preview" ? "preview" : "stable";
}

function isBusyOperation(kind: UpdaterOperationKind): boolean {
  return kind === "checking" || kind === "downloading" || kind === "installing";
}

function useUpdaterInternal(): Updater {
  const [status, setStatus] = useState<UpdateStatus>({ kind: "idle" });
  const operationRef = useRef<UpdaterOperation>({
    epoch: 0,
    requestId: "initial",
    channel: "",
    expectedVersion: "",
    kind: "idle",
  });

  const beginOperation = useCallback((
    channel: string,
    kind: UpdaterOperationKind,
    expectedVersion = "",
  ): UpdaterOperation => {
    const epoch = operationRef.current.epoch + 1;
    const next: UpdaterOperation = {
      epoch,
      requestId: nextUpdaterRequestId(epoch),
      channel: channel ? normalizedChannel(channel) : "",
      expectedVersion,
      kind,
    };
    operationRef.current = next;
    return next;
  }, []);

  const isCurrentOperation = useCallback((operation: UpdaterOperation): boolean => {
    const current = operationRef.current;
    return current.epoch === operation.epoch &&
      current.requestId === operation.requestId &&
      current.channel === operation.channel &&
      current.expectedVersion === operation.expectedVersion;
  }, []);

  const completeOperation = useCallback((operation: UpdaterOperation): void => {
    if (isCurrentOperation(operation)) {
      operationRef.current = { ...operationRef.current, kind: "ready" };
    }
  }, [isCurrentOperation]);

  // A single long-lived subscription advances the state machine through the apply
  // phases. Channel and operation-kind checks prevent a superseded native
  // download/install from publishing into a newly selected channel.
  useEffect(() => {
    return onUpdaterProgress((p) => {
      const operation = operationRef.current;
      if (
        !p.requestId ||
        p.requestId !== operation.requestId ||
        !p.channel ||
        normalizedChannel(p.channel) !== operation.channel ||
        !p.version ||
        p.version !== operation.expectedVersion
      ) return;
      const accepted =
        ((p.phase === "downloading" || p.phase === "verifying") && operation.kind === "downloading") ||
        (p.phase === "downloaded" && (operation.kind === "downloading" || operation.kind === "installing")) ||
        ((p.phase === "authorizing" || p.phase === "installing" || p.phase === "done") && operation.kind === "installing") ||
        (p.phase === "error" && (operation.kind === "downloading" || operation.kind === "installing"));
      if (!accepted) return;
      if (p.phase === "downloaded" || p.phase === "done" || p.phase === "error") {
        operationRef.current = { ...operation, kind: "ready" };
      }
      setStatus((cur) => {
        const info = "info" in cur ? cur.info : undefined;
        if (info && normalizedChannel(info.channel) !== operation.channel) return cur;
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
    const operation = beginOperation(channel, "checking");
    setStatus({ kind: "checking" });
    try {
      const info = await app.CheckUpdate(channel);
      if (!isCurrentOperation(operation)) return;
      if (!info) {
        completeOperation(operation);
        setStatus({ kind: "upToDate", current: "" });
        return;
      }
      const responseChannel = normalizedChannel(info.channel);
      if (operation.channel && responseChannel !== operation.channel) {
        completeOperation(operation);
        setStatus({
          kind: "error",
          message: `update check returned ${responseChannel} for requested ${operation.channel} channel`,
        });
        return;
      }
      operation.channel = responseChannel;
      operation.expectedVersion = info.latest;
      operationRef.current = { ...operation, kind: "ready" };
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
      if (!isCurrentOperation(operation)) return;
      completeOperation(operation);
      setStatus({ kind: "error", message: errMsg(e) });
    }
  }, [beginOperation, completeOperation, isCurrentOperation]);

  const download = useCallback((info: UpdateInfo) => {
    const selectedChannel = normalizedChannel(info.channel);
    const active = operationRef.current;
    if (isBusyOperation(active.kind) || (active.channel && active.channel !== selectedChannel)) return;
    if (!info.canSelfUpdate) {
      void app.OpenDownloadPage();
      return;
    }
    const operation = beginOperation(selectedChannel, "downloading", info.latest);
    setStatus({ kind: "downloading", received: 0, total: info.assetSize, info });
    void app.DownloadUpdateRequest(selectedChannel, info.latest, operation.requestId)
      .then((result) => {
        if (!isCurrentOperation(operation)) return;
        if (
          !result ||
          result.requestId !== operation.requestId ||
          result.version !== info.latest ||
          normalizedChannel(result.channel) !== selectedChannel
        ) {
          completeOperation(operation);
          setStatus({ kind: "available", info });
          return;
        }
        completeOperation(operation);
        setStatus({ kind: "downloaded", info: { ...info, downloaded: true } });
      })
      .catch((e) => {
        if (!isCurrentOperation(operation)) return;
        completeOperation(operation);
        setStatus({ kind: "error", message: errMsg(e), info });
      });
  }, [beginOperation, completeOperation, isCurrentOperation]);

  const install = useCallback(() => {
    const info = "info" in status ? status.info : undefined;
    if (!info) return;
    const selectedChannel = normalizedChannel(info.channel);
    const active = operationRef.current;
    if (isBusyOperation(active.kind) || (active.channel && active.channel !== selectedChannel)) return;
    const operation = beginOperation(selectedChannel, "installing", info.latest);
    // Deb installs start in authorizing; portable/other go straight to installing.
    setStatus(info.requiresElevation || info.installMode === "deb"
      ? { kind: "authorizing", info }
      : { kind: "installing", info });
    void app.InstallUpdateRequest(selectedChannel, info.latest, operation.requestId).catch((e) => {
      if (!isCurrentOperation(operation)) return;
      const message = errMsg(e);
      completeOperation(operation);
      setStatus({ kind: "error", message, info, manualHint: offersManualFallback(message) });
    });
  }, [beginOperation, completeOperation, isCurrentOperation, status]);

  const openDownload = useCallback(() => {
    void app.OpenDownloadPage();
  }, []);

  const reset = useCallback((channel?: "stable" | "preview") => {
    const epoch = operationRef.current.epoch + 1;
    operationRef.current = {
      epoch,
      requestId: nextUpdaterRequestId(epoch),
      channel: channel ? normalizedChannel(channel) : "",
      expectedVersion: "",
      kind: "idle",
    };
    setStatus({ kind: "idle" });
  }, []);

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
