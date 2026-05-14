import { writeFileSync } from "node:fs";
import { Session } from "node:inspector/promises";
import { resolve } from "node:path";

let session: Session | null = null;
let outPath: string | null = null;
let installed = false;
let stopping = false;

function defaultOutPath(): string {
  const stamp = new Date().toISOString().replace(/[:.]/g, "-").replace("Z", "");
  return resolve(process.cwd(), `reasonix-cpu-${stamp}.cpuprofile`);
}

export async function startCpuProfile(pathArg?: string | true): Promise<string> {
  if (session) return outPath ?? defaultOutPath();
  outPath = typeof pathArg === "string" ? resolve(pathArg) : defaultOutPath();
  session = new Session();
  session.connect();
  await session.post("Profiler.enable");
  await session.post("Profiler.start");
  process.stderr.write(`▸ cpu profile recording — will save to ${outPath} on exit\n`);
  return outPath;
}

async function stopAndSave(): Promise<void> {
  if (!session || !outPath || stopping) return;
  stopping = true;
  const s = session;
  const out = outPath;
  session = null;
  try {
    const { profile } = (await s.post("Profiler.stop")) as { profile: unknown };
    writeFileSync(out, JSON.stringify(profile));
    process.stderr.write(`▸ cpu profile saved → ${out}\n`);
  } catch (e) {
    process.stderr.write(`▲ cpu profile save failed: ${(e as Error).message}\n`);
  } finally {
    try {
      s.disconnect();
    } catch {
      /* ignore */
    }
  }
}

export function installCpuProfileExitHandler(): void {
  if (installed) return;
  installed = true;
  const onSignal = (sig: NodeJS.Signals) => {
    void (async () => {
      await stopAndSave();
      process.exit(sig === "SIGINT" ? 130 : 0);
    })();
  };
  for (const sig of ["SIGINT", "SIGTERM", "SIGHUP"] as const) {
    process.on(sig, onSignal);
  }
  // beforeExit fires when the loop is empty — gives us one chance to await.
  process.on("beforeExit", () => {
    void stopAndSave();
  });
}
