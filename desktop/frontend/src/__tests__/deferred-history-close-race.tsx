import { act } from "react";
import { getTranscriptStore } from "../lib/transcriptStore";
import type { HistorySlice } from "../lib/types";
import { historySliceFromMessages } from "./mockHistorySlice";

type DeferredHistory = {
  promise: Promise<HistorySlice>;
  resolve: (value: HistorySlice) => void;
};

type OlderHistoryState = {
  items: Array<{ kind: string; text?: string }>;
  historyOlderLoading: boolean;
  historyOlderError?: string;
};

export async function verifyStaleHistoryFingerprint({
  olderPage,
  loadOlderHistory,
  historyCalls,
  waitFor,
  flushPromises,
  equal,
  getState,
}: {
  olderPage: DeferredHistory;
  loadOlderHistory: () => Promise<boolean> | undefined;
  historyCalls: () => number;
  waitFor: (label: string, predicate: () => boolean) => Promise<void>;
  flushPromises: () => Promise<void>;
  equal: (actual: unknown, expected: unknown, label: string) => void;
  getState: () => OlderHistoryState | undefined;
}) {
  let olderLoad: Promise<boolean> | undefined;
  await act(async () => {
    olderLoad = loadOlderHistory();
    await flushPromises();
  });
  await waitFor("tab-l older page request", () => historyCalls() === 2);
  olderPage.resolve({
    ...historySliceFromMessages(
      "tab-l",
      [{ role: "user", content: "stale older L" }],
      { cursor: "", turns: 12 },
      { revision: 1, digest: "digest-l-v1" },
    ),
    hasOlder: true,
    nextCursor: btoa(JSON.stringify({ v: 1, before: 2 })),
  });
  await act(async () => {
    await olderLoad;
    await flushPromises();
  });
  const state = getState();
  equal(state?.items.some((item) => item.kind === "user" && item.text === "stale older L") ?? false, false, "stale older page is discarded after session fingerprint changes");
  equal(state?.historyOlderLoading, false, "stale older page releases its loading state");
  equal(state?.historyOlderError, "history identity changed", "stale older page enters the explicit retry state instead of silently auto-retrying");
}

export async function verifyDeferredHistoryCloseRace({
  olderPage,
  loadOlderHistory,
  closeTab,
  historyCalls,
  waitFor,
  flushPromises,
  equal,
  sessionPath,
}: {
  olderPage: DeferredHistory;
  loadOlderHistory: () => Promise<boolean> | undefined;
  closeTab: () => Promise<boolean>;
  historyCalls: () => number;
  waitFor: (label: string, predicate: () => boolean) => Promise<void>;
  flushPromises: () => Promise<void>;
  equal: (actual: unknown, expected: unknown, label: string) => void;
  sessionPath: string;
}) {
  let closingOlderLoad: Promise<boolean> | undefined;
  await act(async () => {
    closingOlderLoad = loadOlderHistory();
    await flushPromises();
  });
  await waitFor("tab-l closing older page request", () => historyCalls() === 3);

  const transcriptStore = getTranscriptStore();
  const originalSetPinned = transcriptStore.setPinned.bind(transcriptStore);
  let releasedTabStateCommits = 0;
  transcriptStore.setPinned = (tabId: string, pinned: boolean) => {
    if (tabId === "tab-l") releasedTabStateCommits += 1;
    originalSetPinned(tabId, pinned);
  };
  let tabClosed = false;
  await act(async () => {
    tabClosed = await closeTab();
    await flushPromises();
  });
  equal(tabClosed, true, "tab-l closes while its older page is deferred");

  olderPage.resolve(historySliceFromMessages(
    "tab-l",
    [{ role: "user", content: "late page after close" }],
    { cursor: "", turns: 12 },
    { revision: 2, digest: "digest-l-v2" },
  ));
  await act(async () => {
    await closingOlderLoad;
    await flushPromises();
  });
  transcriptStore.setPinned = originalSetPinned;
  equal(releasedTabStateCommits, 0, "late older-page completion cannot recreate the released tab state");
  equal(transcriptStore.isResident("tab-l", sessionPath), false, "late older-page completion cannot restore the evicted transcript");
}
