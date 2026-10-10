import { useCallback, useEffect, useRef, useState } from "react";
import type { Dispatch, SetStateAction } from "react";
import type { ProviderModelCheckRequest } from "../port/port";
import { checkedFact, type ModelFact, type ModelOrigin } from "./ModelChoice";
import type { Port } from "./Providers";

export function useModelBatch({ port, picked, request, origin, setFacts, draftKey, blocked }: {
  port: Port;
  picked: string[];
  request: Omit<ProviderModelCheckRequest, "model">;
  origin: ModelOrigin;
  setFacts: Dispatch<SetStateAction<Record<string, ModelFact>>>;
  draftKey: string;
  blocked: boolean;
}) {
  const [batch, setBatch] = useState<"idle" | "running" | "stopped">("idle");
  const run = useRef<AbortController | null>(null);

  const stopBatch = useCallback(() => {
    if (!run.current) return false;
    run.current.abort();
    run.current = null;
    setFacts((current) => Object.fromEntries(Object.entries(current).map(([model, fact]) => [model, fact.checking ? { ...fact, checking: false } : fact])));
    setBatch("stopped");
    return true;
  }, [setFacts]);

  const testAll = async () => {
    if (run.current || blocked || picked.length === 0) return;
    const ctl = new AbortController();
    run.current = ctl;
    setBatch("running");
    const queue = [...picked];
    const lane = async () => {
      for (let model = queue.shift(); model !== undefined && !ctl.signal.aborted; model = queue.shift()) {
        const m = model;
        setFacts((current) => ({ ...current, [m]: { ...(current[m] ?? { origin }), checking: true } }));
        let result;
        try {
          result = await port.checkProviderModel({ ...request, model: m });
        } catch {
          result = { model: m, status: "unknown" as const, reason: "network" as const };
        }
        if (ctl.signal.aborted) return;
        setFacts((current) => ({ ...current, [m]: { ...checkedFact(current[m], origin, result), checking: false } }));
      }
    };
    await Promise.all([lane(), lane()]);
    if (run.current === ctl && !ctl.signal.aborted) {
      run.current = null;
      setBatch("idle");
    }
  };

  useEffect(() => {
    if (!stopBatch()) setBatch((current) => current === "stopped" ? "idle" : current);
  }, [draftKey, port, stopBatch]);
  useEffect(() => () => run.current?.abort(), []);

  return { batch, testAll };
}
