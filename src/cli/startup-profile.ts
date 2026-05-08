import { performance } from "node:perf_hooks";

interface PhaseMark {
  name: string;
  t: number;
}

const marks: PhaseMark[] = [];
let dumped = false;

function envFlag(): boolean {
  const v = process.env.REASONIX_PROFILE_STARTUP;
  return v === "1" || v === "true" || v === "yes";
}

export function isStartupProfileEnabled(): boolean {
  return envFlag();
}

export function markPhase(name: string): void {
  if (!envFlag()) return;
  marks.push({ name, t: performance.now() });
}

export function dumpStartupProfile(stream: NodeJS.WriteStream = process.stderr): void {
  if (!envFlag() || dumped || marks.length === 0) return;
  dumped = true;
  const totalMs = marks[marks.length - 1]!.t;
  const widest = String(Math.round(totalMs)).length;
  const lines: string[] = ["[startup-profile]"];
  let prev = 0;
  for (const m of marks) {
    const cum = Math.round(m.t).toString().padStart(widest);
    const delta = Math.round(m.t - prev);
    lines.push(`  ${cum}ms  ${m.name.padEnd(28)}  (+${delta})`);
    prev = m.t;
  }
  lines.push(
    `─── ${Math.round(totalMs)}ms total · last phase ${marks[marks.length - 1]!.name} · set REASONIX_PROFILE_STARTUP=0 to silence`,
  );
  stream.write(`${lines.join("\n")}\n`);
}

export function _resetForTests(): void {
  marks.length = 0;
  dumped = false;
}
