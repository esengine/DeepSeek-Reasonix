import { spawn } from "node:child_process";
import {
  type InstallSource,
  VERSION,
  compareVersions,
  detectInstallSource,
  detectNpmInstallPrefix,
  getLatestVersion,
} from "../../version.js";

export type UpdateAction =
  | "up-to-date"
  | "newer-local"
  | "npx-hint"
  | "manual-hint"
  | "run-install";

export interface UpdatePlan {
  action: UpdateAction;
  /** Human-readable summary; the CLI prints this verbatim. */
  message: string;
  command?: string[];
}

export interface PlanUpdateInput {
  current: string;
  latest: string;
  installSource: InstallSource;
  /** Pin npm to this prefix so nvm/fnm can't redirect the install. */
  npmPrefix?: string | null;
}

export const MANUAL_UPDATE_COMMANDS: readonly string[] = [
  "npm install -g reasonix@latest",
  "bun add -g reasonix",
  "pnpm add -g reasonix@latest",
  "yarn global add reasonix@latest",
];

/** Pure decision — split out so tests don't need to spawn child processes or hit the network. */
export function planUpdate(input: PlanUpdateInput): UpdatePlan {
  const diff = compareVersions(input.current, input.latest);
  if (diff > 0) {
    return {
      action: "newer-local",
      message: `current (${input.current}) is newer than the published ${input.latest} — nothing to do.`,
    };
  }
  if (diff === 0) {
    return { action: "up-to-date", message: `reasonix ${input.current} is up to date.` };
  }
  if (input.installSource === "npx") {
    return {
      action: "npx-hint",
      message: [
        `reasonix ${input.latest} is available.`,
        "you're running via npx — the next `npx reasonix ...` launch will auto-fetch",
        "the latest (npx caches packages for a short window). to force a refresh",
        "sooner, clear the cache: `npm cache clean --force`.",
      ].join("\n"),
    };
  }
  if (input.installSource === "unknown") {
    return {
      action: "manual-hint",
      message: [
        `reasonix ${input.latest} is available, but the install source could not be determined automatically.`,
        "run one of these manually based on how you installed reasonix:",
        ...MANUAL_UPDATE_COMMANDS.map((c) => `  ${c}`),
      ].join("\n"),
    };
  }
  const command = buildUpdateCommand(input.installSource, input.npmPrefix ?? null);
  return {
    action: "run-install",
    message: `upgrading reasonix ${input.current} → ${input.latest} (via ${input.installSource})`,
    command,
  };
}

function buildUpdateCommand(
  source: Exclude<InstallSource, "npx" | "unknown">,
  npmPrefix: string | null,
): string[] {
  switch (source) {
    case "npm":
      return npmPrefix
        ? ["npm", "--prefix", npmPrefix, "install", "-g", "reasonix@latest"]
        : ["npm", "install", "-g", "reasonix@latest"];
    case "bun":
      return ["bun", "add", "-g", "reasonix"];
    case "pnpm":
      return ["pnpm", "add", "-g", "reasonix@latest"];
    case "yarn":
      return ["yarn", "global", "add", "reasonix@latest"];
  }
}

export interface UpdateCommandOptions {
  /** Skip spawning the package manager; print the decision only. */
  dryRun?: boolean;
  /** Test seam: override the registry lookup. Returns null = offline. */
  fetchLatest?: () => Promise<string | null>;
  /** Test seam: override the install-source detector. */
  detectSource?: () => InstallSource;
  /** Test seam: override the npm prefix detector. */
  detectPrefix?: () => string | null;
  /** Test seam: override the spawner. Must return exit code. */
  spawnInstall?: (argv: string[]) => Promise<number>;
  /** Test seam: stdout writer. */
  write?: (msg: string) => void;
  /** Test seam: process exit — tests don't want to tear down vitest. */
  exit?: (code: number) => void;
}

function defaultSpawn(argv: string[]): Promise<number> {
  return new Promise((resolve, reject) => {
    // `shell: true` on Windows is what lets `npm` resolve to `npm.cmd`
    // without routing through our `prepareSpawn` helper. The args here
    // are literal strings under our control — no user input flows in,
    // so injection is not a concern. Avoiding `prepareSpawn` keeps
    // this command free of a dep on the shell tools module.
    const child = spawn(argv[0]!, argv.slice(1), {
      stdio: "inherit",
      shell: process.platform === "win32",
    });
    child.once("error", reject);
    child.once("exit", (code) => resolve(code ?? 1));
  });
}

export async function updateCommand(opts: UpdateCommandOptions = {}): Promise<void> {
  const write = opts.write ?? ((m: string) => process.stdout.write(m));
  const exit = opts.exit ?? ((c: number) => process.exit(c));
  const fetchLatest = opts.fetchLatest ?? (() => getLatestVersion({ force: true }));
  const detectSource = opts.detectSource ?? (() => detectInstallSource());
  const detectPrefix = opts.detectPrefix ?? (() => detectNpmInstallPrefix());
  const doSpawn = opts.spawnInstall ?? defaultSpawn;

  write(`current: reasonix ${VERSION}\n`);
  const latest = await fetchLatest();
  if (!latest) {
    write("could not reach registry.npmjs.org — check your network.\n");
    exit(1);
    return;
  }
  write(`latest:  reasonix ${latest}\n`);

  const installSource = detectSource();
  const npmPrefix = installSource === "npm" ? detectPrefix() : null;
  const plan = planUpdate({ current: VERSION, latest, installSource, npmPrefix });
  write(`\n${plan.message}\n`);

  if (plan.action === "manual-hint") {
    exit(1);
    return;
  }
  if (plan.action !== "run-install" || !plan.command) return;
  if (opts.dryRun) {
    write(`(dry run) would run: ${plan.command.join(" ")}\n`);
    return;
  }
  write(`\nrunning: ${plan.command.join(" ")}\n`);
  const code = await doSpawn(plan.command);
  if (code !== 0) {
    write(`\n${plan.command[0]} exited with code ${code}. upgrade did not complete.\n`);
    exit(code);
  }
}
