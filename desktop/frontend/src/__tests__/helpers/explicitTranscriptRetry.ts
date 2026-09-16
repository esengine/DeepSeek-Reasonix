import { act } from "react";
import type { useController } from "../../lib/useController";

export async function verifyExplicitTranscriptRetry(options: {
  controller: () => ReturnType<typeof useController> | undefined;
  historyCalls: () => number;
  ancillaryCalls: () => number;
  releaseAncillary: () => void;
  waitFor: (label: string, predicate: () => boolean) => Promise<void>;
  flush: () => Promise<void>;
  ok: (value: boolean, label: string) => void;
}) {
  const { controller, historyCalls, ancillaryCalls, releaseAncillary, waitFor, flush, ok } = options;
  await waitFor("Follow cut remains independent of metadata", () => controller()?.state.items.some(item => item.kind === "user" && item.text === "stale M v1") ?? false);
  ok(historyCalls() === 1, "metadata does not attach a later version to earlier body data");
  await waitFor("old ancillary read is pending", () => ancillaryCalls() === 1);
  let retry: Promise<void> | undefined;
  await act(async () => { retry = controller()?.retrySessionHistory("tab-m"); await flush(); });
  try {
    await waitFor("explicit retry requests a fresh cut before ancillary completion", () => historyCalls() === 2);
    await act(async () => { await retry; await flush(); });
    ok(controller()?.state.items.some(item => item.kind === "user" && item.text === "history M v2") === true, "explicit retry installs the new snapshot");
  } finally {
    await act(async () => { releaseAncillary(); await flush(); });
  }
  ok(controller()?.state.context.used !== 99999, "superseded ancillary completion cannot overwrite the retry");
}
