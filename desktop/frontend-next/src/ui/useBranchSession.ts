import { useCallback, type MutableRefObject } from "react";
import type { AgentPort } from "../port/port";
import type { RuntimeView } from "../port/hub";
import { t } from "../i18n";

interface Inputs {
  panePorts: Map<string, AgentPort>;
  runtimes: RuntimeView[];
  runsRef: MutableRefObject<Record<string, { live: boolean }>>;
  openPane: (target: { root: string; sessionPath: string }) => Promise<unknown>;
}

/** Branches an open conversation from its latest finished reply. While a turn
 *  runs the highest checkpoint is that turn's own, so the one before it is the
 *  latest the kernel can cut. */
export function useBranchSession({ panePorts, runtimes, runsRef, openPane }: Inputs) {
  return useCallback(
    async (runtimeId: string, name: string) => {
      const port = panePorts.get(runtimeId);
      const rt = runtimes.find((r) => r.id === runtimeId);
      if (!port || !rt) return;
      const turns = (await port.checkpoints()).map((c) => c.turn).sort((a, b) => a - b);
      const closed = runsRef.current[runtimeId]?.live ? turns.slice(0, -1) : turns;
      const turn = closed[closed.length - 1];
      if (turn === undefined) throw new Error(t("session.there_is_no_finished_reply"));
      await openPane({ root: rt.root, sessionPath: await port.branchAt(turn, name) });
    },
    [panePorts, runtimes, runsRef, openPane],
  );
}
