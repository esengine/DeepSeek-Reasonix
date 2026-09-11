import assert from "node:assert/strict";
import { test } from "node:test";
import type { BrowserDownloadView } from "../../shared/ipc.js";
import { DownloadTracker, type DownloadItemLike } from "./downloads.js";
import { silentLog } from "./fakeGuestViews.js";

// A fake clock: scheduled callbacks fire only when advance() runs them, so
// the wait logic is tested without real time passing.
function fakeClock() {
  let now = 0;
  const pending = new Map<number, { at: number; run: () => void }>();
  let nextId = 1;
  const setTimeoutFake = ((run: () => void, ms: number) => {
    const id = nextId++;
    pending.set(id, { at: now + ms, run });
    return id;
  }) as unknown as typeof setTimeout;
  const clearTimeoutFake = ((id: number) => {
    pending.delete(id);
  }) as unknown as typeof clearTimeout;
  return {
    setTimeout: setTimeoutFake,
    clearTimeout: clearTimeoutFake,
    advance(ms: number): void {
      now += ms;
      for (const [id, timer] of [...pending]) {
        if (timer.at > now) continue;
        pending.delete(id);
        timer.run();
      }
    },
    get pendingCount(): number {
      return pending.size;
    },
  };
}

class FakeDownloadItem implements DownloadItemLike {
  savePath = "";
  received = 0;
  state = "progressing";
  private readonly listeners = new Map<string, Array<(event: unknown, state: string) => void>>();

  constructor(
    readonly url: string,
    readonly filename: string,
    readonly total: number,
  ) {}

  getURL(): string {
    return this.url;
  }

  getFilename(): string {
    return this.filename;
  }

  getSavePath(): string {
    return this.savePath;
  }

  setSavePath(path: string): void {
    this.savePath = path;
  }

  getState(): "progressing" {
    return "progressing";
  }

  getReceivedBytes(): number {
    return this.received;
  }

  getTotalBytes(): number {
    return this.total;
  }

  on(event: "updated" | "done", listener: (event: unknown, state: string) => void): void {
    const list = this.listeners.get(event) ?? [];
    list.push(listener);
    this.listeners.set(event, list);
  }

  fire(event: "updated" | "done", state: string, received = this.received): void {
    this.received = received;
    for (const listener of this.listeners.get(event) ?? []) listener({}, state);
  }
}

function setup(options: { existing?: string[] } = {}) {
  const clock = fakeClock();
  const updates: BrowserDownloadView[] = [];
  const made: string[] = [];
  const tracker = new DownloadTracker({
    tabForWebContents: (id) => (id === 7 ? { id: "tab-1", taskId: "task-1" } : id === 8 ? { id: "tab-2", taskId: "task-2" } : undefined),
    defaultDirectory: (taskId) => `/downloads/${taskId}`,
    onUpdate: (download) => updates.push(download),
    log: silentLog,
    exists: (path) => (options.existing ?? []).includes(path),
    mkdir: (path) => made.push(path),
    setTimeout: clock.setTimeout,
    clearTimeout: clock.clearTimeout,
  });
  return { clock, updates, made, tracker };
}

test("will-download routes the file into the task directory with a unique name", () => {
  const { tracker, updates, made } = setup({ existing: ["/scratch/report.pdf", "/scratch/report-1.pdf"] });
  tracker.setTaskDirectory("task-1", "/scratch");
  const item = new FakeDownloadItem("https://a.test/report.pdf", "../report.pdf", 100);
  tracker.handleWillDownload(item, 7);
  assert.deepEqual(made, ["/scratch"]);
  assert.equal(item.savePath, "/scratch/report-2.pdf");
  assert.deepEqual(tracker.list("tab-1"), [
    { id: "dl-1", tabId: "tab-1", url: "https://a.test/report.pdf", filename: "report-2.pdf", path: "/scratch/report-2.pdf", state: "progressing", received: 0, total: 100 },
  ]);
  assert.deepEqual(updates.at(-1), tracker.list("tab-1")[0]);

  const unknown = new FakeDownloadItem("https://a.test/x", "x", 1);
  tracker.handleWillDownload(unknown, 99);
  assert.equal(unknown.savePath, "", "downloads from unknown webContents are dropped");

  const other = new FakeDownloadItem("https://b.test/f.bin", "f.bin", 5);
  tracker.handleWillDownload(other, 8);
  assert.equal(other.savePath, "/downloads/task-2/f.bin", "tasks without a scratch directory use the shell default");
});

test("concurrent downloads reserve the same filename before either reaches disk", () => {
  const { tracker } = setup();
  const first = new FakeDownloadItem("https://a.test/one", "report.txt", 10);
  const second = new FakeDownloadItem("https://a.test/two", "report.txt", 10);
  tracker.setTaskDirectory("task-1", "/scratch");
  tracker.handleWillDownload(first, 7);
  tracker.handleWillDownload(second, 7);
  assert.equal(first.savePath, "/scratch/report.txt");
  assert.equal(second.savePath, "/scratch/report-1.txt");
});

test("progress and terminal states are tracked and non-progressing rows are forgotten", () => {
  const { tracker } = setup();
  const item = new FakeDownloadItem("https://a.test/f.zip", "f.zip", 10);
  tracker.handleWillDownload(item, 7);
  item.fire("updated", "progressing", 4);
  assert.equal(tracker.list("tab-1")[0].received, 4);
  item.fire("done", "completed", 10);
  const done = tracker.list("tab-1")[0];
  assert.equal(done.state, "completed");
  assert.deepEqual(tracker.hostList("tab-1"), [{ id: done.id, url: done.url, path: done.path, state: "completed", bytes: 10 }]);

  const live = new FakeDownloadItem("https://a.test/g.zip", "g.zip", 10);
  tracker.handleWillDownload(live, 7);
  tracker.forgetTab("tab-1");
  assert.deepEqual(tracker.list("tab-1").map((d) => d.id), [live.savePath ? "dl-2" : ""], "only the still-progressing download survives");
});

test("wait resolves early when nothing is in progress", async () => {
  const { tracker, clock } = setup();
  assert.deepEqual(await tracker.wait("tab-1", 5000), []);
  assert.equal(clock.pendingCount, 0, "no timer was scheduled");
  const item = new FakeDownloadItem("https://a.test/f", "f", 1);
  tracker.handleWillDownload(item, 7);
  assert.deepEqual((await tracker.wait("tab-1", 0)).map((d) => d.state), ["progressing"], "a zero wait never blocks");
});

test("wait holds until the last download settles, then resolves without the timer", async () => {
  const { tracker, clock } = setup();
  const first = new FakeDownloadItem("https://a.test/a", "a", 1);
  const second = new FakeDownloadItem("https://a.test/b", "b", 1);
  tracker.handleWillDownload(first, 7);
  tracker.handleWillDownload(second, 7);
  let settled = false;
  const waiting = tracker.wait("tab-1", 5000).then((downloads) => {
    settled = true;
    return downloads;
  });
  assert.equal(clock.pendingCount, 1);
  first.fire("done", "cancelled", 0);
  await Promise.resolve();
  assert.equal(settled, false, "another download is still in progress");
  second.fire("done", "completed", 1);
  const result = await waiting;
  assert.deepEqual(result.map((d) => d.state), ["cancelled", "completed"]);
  assert.equal(clock.pendingCount, 0, "the expiry timer was cancelled");
});

test("wait expires on the fake clock when downloads never finish", async () => {
  const { tracker, clock } = setup();
  const item = new FakeDownloadItem("https://a.test/slow", "slow", 1);
  tracker.handleWillDownload(item, 7);
  let settled = false;
  const waiting = tracker.wait("tab-1", 2000).then((downloads) => {
    settled = true;
    return downloads;
  });
  clock.advance(1999);
  await Promise.resolve();
  assert.equal(settled, false);
  clock.advance(1);
  const result = await waiting;
  assert.deepEqual(result.map((d) => d.state), ["progressing"], "the caller gets the live view at expiry");
});
