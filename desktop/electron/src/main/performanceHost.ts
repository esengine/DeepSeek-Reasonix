import type { BrowserWindow, Dialog } from "electron";
import { analyseInWorker } from "./profileAnalysisHost.js";
import { RendererDiagnostics } from "./rendererDiagnostics.js";

type HeapResult = { status: "saved" | "cancelled" | "busy" | "unavailable" | "failed" };
export function createPerformanceHost(deps: {
  window(): BrowserWindow | null;
  dialog: Pick<Dialog, "showMessageBox" | "showSaveDialog">;
  workerPath: string;
  locale(): string;
  /** Controlled native fixtures can pin activity without changing production policy. */
  isForeground?(): boolean;
}) {
  const isForeground = deps.isForeground ?? (() => Boolean(deps.window()?.isVisible() && deps.window()?.isFocused()));
  const cpu = new RendererDiagnostics({
    target: () => deps.window()?.webContents ?? null,
    isForeground,
    onInvalidated: (cancel) => {
      const win = deps.window();
      if (!win) { cancel(); return () => {}; }
      const contents = win.webContents;
      const navigation = (_event: unknown, _url: string, _inPlace: boolean, main: boolean) => { if (main) cancel(); };
      const cleanup = () => {
        win.removeListener("blur", cancel);
        win.removeListener("hide", cancel);
        win.removeListener("closed", cancel);
        contents.removeListener("destroyed", cancel);
        contents.removeListener("render-process-gone", cancel);
        contents.removeListener("did-start-navigation", navigation);
      };
      try {
        win.on("blur", cancel);
        win.on("hide", cancel);
        win.on("closed", cancel);
        contents.on("destroyed", cancel);
        contents.on("render-process-gone", cancel);
        contents.on("did-start-navigation", navigation);
      } catch (error) {
        cleanup();
        throw error;
      }
      return cleanup;
    },
    analyse: (profile) => analyseInWorker(profile, deps.workerPath),
  });
  let heapBusy = false;
  let disposed = false;
  return {
    captureRendererProfile: (requestId?: string) => heapBusy ? Promise.resolve({ status: "busy" as const }) : cpu.capture(requestId),
    cancelRendererProfile: (requestId?: string) => { if (requestId) cpu.cancel(requestId); },
    dispose: () => { disposed = true; cpu.dispose(); },
    async exportHeapSnapshot(): Promise<HeapResult> {
      if (disposed) return { status: "unavailable" };
      if (heapBusy || cpu.busy) return { status: "busy" };
      const win = deps.window();
      if (!win || !isForeground()) return { status: "unavailable" };
      heapBusy = true;
      let invalidated = false;
      const contents = win.webContents;
      const navigation = (_event: unknown, _url: string, _inPlace: boolean, main: boolean) => { if (main) invalidated = true; };
      const valid = () => !disposed && !invalidated && !contents.isDestroyed() && deps.window() === win;
      try {
        contents.on("did-start-navigation", navigation);
        const zh = deps.locale().startsWith("zh");
        const confirmation = await deps.dialog.showMessageBox(win, {
          type: "warning", title: zh ? "保存内存快照" : "Save heap snapshot",
          message: zh ? "内存快照可能包含聊天、代码和密钥等敏感内容，生成时会暂停界面，并可能占用较多磁盘空间。仅保存到本地，不自动上传。" : "A heap snapshot can contain chats, code and secrets. Capturing it pauses the interface and can use substantial disk space. It is saved locally and is not uploaded automatically.",
          buttons: zh ? ["取消", "继续保存"] : ["Cancel", "Continue"], defaultId: 0, cancelId: 0,
        });
        if (confirmation.response !== 1 || !valid()) return { status: "cancelled" };
        const destination = await deps.dialog.showSaveDialog(win, {
          title: zh ? "保存内存快照" : "Save heap snapshot", defaultPath: "reasonix-renderer.heapsnapshot",
          filters: [{ name: "Heap snapshot", extensions: ["heapsnapshot"] }],
        });
        if (destination.canceled || !destination.filePath || !valid()) return { status: "cancelled" };
        // Electron cannot preempt this operation. Retain the busy lease until
        // its actual settlement, rather than pretending a timeout cancelled it.
        await contents.takeHeapSnapshot(destination.filePath);
        return { status: "saved" };
      } catch {
        return { status: "failed" };
      } finally {
        try { contents.removeListener("did-start-navigation", navigation); } catch { /* Best effort after renderer exit. */ }
        heapBusy = false;
      }
    },
  };
}

export type PerformanceHost = ReturnType<typeof createPerformanceHost>;
