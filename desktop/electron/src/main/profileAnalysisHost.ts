import { Worker } from "node:worker_threads";
import type { CpuProfile, ProfileFrame } from "./rendererDiagnostics.js";

export function analyseInWorker(profile: CpuProfile, workerPath: string): Promise<ProfileFrame[]> {
  if (profile.nodes.length > 20_000 || (profile.samples?.length ?? 0) > 100_000) return Promise.reject(new Error("profile exceeds analysis budget"));
  return new Promise((resolve, reject) => {
    const worker = new Worker(workerPath, { workerData: profile, resourceLimits: { maxOldGenerationSizeMb: 32 } });
    const timer = setTimeout(() => finish(new Error("profile analysis timeout")), 1500);
    let settled = false;
    const finish = (error?: Error, frames?: ProfileFrame[]) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      void worker.terminate();
      if (error) reject(error); else resolve(frames ?? []);
    };
    worker.once("message", (frames: ProfileFrame[]) => finish(undefined, frames));
    worker.once("error", (error) => finish(error instanceof Error ? error : new Error("profile analysis failed")));
    worker.once("exit", () => finish(new Error("profile analysis exited without a result")));
  });
}
