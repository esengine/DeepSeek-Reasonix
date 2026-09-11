import type { CpuProfile, ProfileFrame } from "./rendererDiagnostics.js";

export function analyseProfile(profile: CpuProfile): ProfileFrame[] {
  const samples = profile.samples ?? [];
  if (profile.nodes.length > 20_000 || samples.length > 100_000) throw new Error("profile exceeds analysis budget");
  const nodes = new Map(profile.nodes.map((node) => [node.id, node.callFrame]));
  const rows = new Map<number, ProfileFrame>();
  for (let i = 0; i < samples.length; i++) {
    const id = samples[i];
    const frame = nodes.get(id);
    if (!frame) continue;
    // Built application scripts only: omit eval, external URLs and user paths.
    if (!frame.url.startsWith("reasonix://app/")) continue;
    const file = frame.url.split("/").pop()?.split(/[?#]/)[0] ?? "";
    if (!/^[\w.-]+\.m?js$/.test(file)) continue;
    const row = rows.get(id) ?? { label: `${frame.functionName.replace(/[\r\n]/g, " ").slice(0, 100) || "(anonymous)"} (${file}:${frame.lineNumber + 1})`, samples: 0, selfMs: 0 };
    row.samples++;
    row.selfMs += Math.max(0, Number.isFinite(profile.timeDeltas?.[i]) ? profile.timeDeltas![i] / 1000 : 0);
    rows.set(id, row);
  }
  return [...rows.values()].sort((a, b) => b.selfMs - a.selfMs || b.samples - a.samples).slice(0, 8);
}
