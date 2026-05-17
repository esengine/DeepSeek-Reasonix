import { execSync } from "node:child_process";
import { existsSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

export type RendererSource =
  | "env-cmd"
  | "env-bin"
  | "optional-dep"
  | "prebuilt-release"
  | "prebuilt-debug"
  | "cargo"
  | null;

export interface ResolvedRenderer {
  command: readonly string[];
  inputCommand: readonly string[];
  source: RendererSource;
}

export interface ResolverIO {
  envCmd(name: string): readonly string[] | undefined;
  envBin(): string | undefined;
  hasFile(path: string): boolean;
  resolveOptionalDep(platform: string, arch: string): string | undefined;
  findReasonixSourceTree(): string | undefined;
  hasCargo(): boolean;
  platform: NodeJS.Platform;
}

let cached: ResolvedRenderer | undefined;

export function resolveRenderer(): ResolvedRenderer {
  if (cached) return cached;
  cached = resolveRendererWith(defaultIO());
  return cached;
}

export function resetResolverCache(): void {
  cached = undefined;
}

export function resolveRendererWith(io: ResolverIO): ResolvedRenderer {
  const base = pickBase(io);
  const renderEnv = io.envCmd("REASONIX_RENDER_CMD");
  const inputEnv = io.envCmd("REASONIX_INPUT_CMD");
  if (!renderEnv && !inputEnv) return base;
  return {
    command: renderEnv ?? base.command,
    inputCommand: inputEnv ?? base.inputCommand,
    source: base.source ?? "env-cmd",
  };
}

function pickBase(io: ResolverIO): ResolvedRenderer {
  const binEnv = io.envBin();
  if (binEnv && io.hasFile(binEnv)) return fromBinary(binEnv, "env-bin");

  const optional = io.resolveOptionalDep(io.platform, process.arch);
  if (optional) return fromBinary(optional, "optional-dep");

  const sourceRoot = io.findReasonixSourceTree();
  if (sourceRoot) {
    const binName = io.platform === "win32" ? "reasonix-render.exe" : "reasonix-render";
    const release = join(sourceRoot, "target", "release", binName);
    if (io.hasFile(release)) return fromBinary(release, "prebuilt-release");
    const debug = join(sourceRoot, "target", "debug", binName);
    if (io.hasFile(debug)) return fromBinary(debug, "prebuilt-debug");
    if (io.hasCargo()) return fromCargo();
  }

  return { command: [], inputCommand: [], source: null };
}

function fromBinary(bin: string, source: RendererSource): ResolvedRenderer {
  return { command: [bin], inputCommand: [bin, "--emit-input"], source };
}

function fromCargo(): ResolvedRenderer {
  const cmd = ["cargo", "run", "--quiet", "--bin", "reasonix-render"];
  return { command: cmd, inputCommand: [...cmd, "--", "--emit-input"], source: "cargo" };
}

function defaultIO(): ResolverIO {
  return {
    envCmd: parseEnvCmd,
    envBin: () => process.env.REASONIX_RENDER_BIN,
    hasFile: existsSync,
    resolveOptionalDep: locateOptionalDepBinary,
    findReasonixSourceTree: detectReasonixSourceTree,
    hasCargo: detectCargo,
    platform: process.platform,
  };
}

function parseEnvCmd(name: string): readonly string[] | undefined {
  const raw = process.env[name];
  if (!raw || raw.length === 0) return undefined;
  try {
    const parsed = JSON.parse(raw);
    if (Array.isArray(parsed) && parsed.every((p) => typeof p === "string")) return parsed;
  } catch {
    /* not JSON — caller treats as absent */
  }
  return undefined;
}

function locateOptionalDepBinary(platform: NodeJS.Platform, arch: string): string | undefined {
  const pkg = `@reasonix/render-${platform}-${arch}`;
  try {
    const require_ = createRequire(import.meta.url);
    const pkgJson = require_.resolve(`${pkg}/package.json`);
    const binName = platform === "win32" ? "reasonix-render.exe" : "reasonix-render";
    const binPath = join(dirname(pkgJson), "bin", binName);
    return existsSync(binPath) ? binPath : undefined;
  } catch {
    return undefined;
  }
}

function detectReasonixSourceTree(): string | undefined {
  let dir = dirname(fileURLToPath(import.meta.url));
  for (let i = 0; i < 12; i++) {
    if (existsSync(join(dir, "crates", "reasonix-render", "Cargo.toml"))) return dir;
    const parent = dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  return undefined;
}

function detectCargo(): boolean {
  try {
    execSync("cargo --version", { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
}
