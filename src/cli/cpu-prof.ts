import { writeFileSync } from "node:fs";
import { Session } from "node:inspector/promises";
import { resolve } from "node:path";
import { gzipSync } from "node:zlib";

let session: Session | null = null;
let outPath: string | null = null;
let signalHandlerInstalled = false;
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
  installSignalHandler();
  return outPath;
}

export async function stopAndSaveCpuProfile(): Promise<void> {
  if (!session || !outPath || stopping) return;
  stopping = true;
  const s = session;
  const baseOut = outPath;
  session = null;
  try {
    const { profile } = (await s.post("Profiler.stop")) as { profile: unknown };
    const json = JSON.stringify(profile);
    const gz = gzipSync(json);
    const gzPath = `${baseOut}.gz`;
    writeFileSync(gzPath, gz);
    const mb = (gz.length / (1024 * 1024)).toFixed(2);
    process.stderr.write(
      `▸ cpu profile saved → ${gzPath} (${mb} MB gzipped)\n  drag into a GitHub issue comment, or:\n  gh issue comment <N> --repo esengine/DeepSeek-Reasonix -F "${gzPath}"\n`,
    );
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

function installSignalHandler(): void {
  if (signalHandlerInstalled) return;
  signalHandlerInstalled = true;
  for (const sig of ["SIGINT", "SIGTERM", "SIGHUP"] as const) {
    process.on(sig, () => {
      void (async () => {
        await stopAndSaveCpuProfile();
        process.exit(sig === "SIGINT" ? 130 : 0);
      })();
    });
  }
}
