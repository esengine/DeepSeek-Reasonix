import { existsSync, mkdirSync } from "node:fs";
import { basename, extname, join } from "node:path";
import type { BrowserDownloadState, BrowserDownloadView } from "../../shared/ipc.js";
import type { Logger } from "../log.js";

export interface DownloadItemLike {
  getURL(): string;
  getFilename(): string;
  getSavePath(): string;
  setSavePath(path: string): void;
  getState(): BrowserDownloadState;
  getReceivedBytes(): number;
  getTotalBytes(): number;
  on(event: "updated" | "done", listener: (event: unknown, state: string) => void): unknown;
}

export interface DownloadTrackerDeps {
  tabForWebContents(webContentsId: number): { id: string; taskId: string } | undefined;
  defaultDirectory(taskId: string): string;
  onUpdate(download: BrowserDownloadView): void;
  log: Logger;
  exists?(path: string): boolean;
  mkdir?(path: string): void;
  setTimeout?: typeof setTimeout;
  clearTimeout?: typeof clearTimeout;
}

export interface HostDownload {
  id: string;
  url: string;
  path: string;
  state: BrowserDownloadState;
  bytes: number;
}

interface Waiter {
  tabId: string;
  resolve(): void;
}

export class DownloadTracker {
  private readonly downloads = new Map<string, BrowserDownloadView>();
  private readonly taskDirectories = new Map<string, string>();
  private readonly waiters = new Set<Waiter>();
  private readonly reservedPaths = new Set<string>();
  private counter = 0;

  constructor(private readonly deps: DownloadTrackerDeps) {}

  // Screenshots and acts name the task's scratch directory; downloads of
  // that task's tabs land there so Go can hand the file over as it does
  // captures. Without one the shell's own per-task folder is used.
  setTaskDirectory(taskId: string, directory: string): void {
    this.taskDirectories.set(taskId, directory);
  }

  handleWillDownload(item: DownloadItemLike, webContentsId: number): void {
    const tab = this.deps.tabForWebContents(webContentsId);
    if (!tab) {
      this.deps.log.warn(`download from unknown webContents ${webContentsId} dropped`);
      return;
    }
    const directory = this.taskDirectories.get(tab.taskId) ?? this.deps.defaultDirectory(tab.taskId);
    try {
      (this.deps.mkdir ?? ((path: string) => mkdirSync(path, { recursive: true })))(directory);
    } catch (error) {
      this.deps.log.warn(`download directory ${directory} unavailable: ${String(error)}`);
    }
    const path = this.uniquePath(directory, item.getFilename());
    this.reservedPaths.add(path);
    item.setSavePath(path);
    this.counter += 1;
    const id = `dl-${this.counter}`;
    const record: BrowserDownloadView = {
      id,
      tabId: tab.id,
      url: item.getURL(),
      filename: basename(path),
      path,
      state: "progressing",
      received: 0,
      total: item.getTotalBytes(),
    };
    this.downloads.set(id, record);
    this.deps.onUpdate({ ...record });
    item.on("updated", (_event, state) => this.update(record, item, state));
    item.on("done", (_event, state) => {
      this.update(record, item, state);
      this.settle();
    });
  }

  list(tabId: string): BrowserDownloadView[] {
    return [...this.downloads.values()].filter((download) => download.tabId === tabId).map((download) => ({ ...download }));
  }

  hostList(tabId: string): HostDownload[] {
    return this.list(tabId).map((download) => ({ id: download.id, url: download.url, path: download.path, state: download.state, bytes: download.received }));
  }

  // Resolves once no download of the tab is in progress or the wait expires.
  wait(tabId: string, waitForMs: number): Promise<HostDownload[]> {
    if (waitForMs <= 0 || !this.inProgress(tabId)) return Promise.resolve(this.hostList(tabId));
    const schedule = this.deps.setTimeout ?? setTimeout;
    const cancel = this.deps.clearTimeout ?? clearTimeout;
    return new Promise((resolve) => {
      const waiter: Waiter = {
        tabId,
        resolve: () => {
          cancel(timer);
          this.waiters.delete(waiter);
          resolve(this.hostList(tabId));
        },
      };
      const timer = schedule(() => waiter.resolve(), waitForMs);
      this.waiters.add(waiter);
    });
  }

  forgetTab(tabId: string): void {
    for (const [id, download] of this.downloads) {
      if (download.tabId === tabId && download.state !== "progressing") this.downloads.delete(id);
    }
  }

  private inProgress(tabId: string): boolean {
    return this.list(tabId).some((download) => download.state === "progressing");
  }

  private update(record: BrowserDownloadView, item: DownloadItemLike, state: string): void {
    record.state = state === "completed" || state === "cancelled" || state === "interrupted" || state === "progressing" ? state : item.getState();
    record.received = item.getReceivedBytes();
    record.total = item.getTotalBytes();
    const savePath = item.getSavePath();
    if (savePath !== "") {
      record.path = savePath;
      record.filename = basename(savePath);
    }
    this.deps.onUpdate({ ...record });
    if (record.state === "cancelled" || record.state === "interrupted") this.reservedPaths.delete(record.path);
  }

  private settle(): void {
    for (const waiter of [...this.waiters]) {
      if (!this.inProgress(waiter.tabId)) waiter.resolve();
    }
  }

  private uniquePath(directory: string, filename: string): string {
    const exists = this.deps.exists ?? existsSync;
    const safe = basename(filename) || "download";
    const ext = extname(safe);
    const stem = ext ? safe.slice(0, -ext.length) : safe;
    let candidate = join(directory, safe);
    for (let n = 1; exists(candidate) || this.reservedPaths.has(candidate); n += 1) candidate = join(directory, `${stem}-${n}${ext}`);
    return candidate;
  }
}
