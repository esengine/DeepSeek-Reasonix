import { existsSync, readFileSync, renameSync, writeFileSync, mkdirSync } from "node:fs";
import { dirname, join } from "node:path";

export type GraphicsOverride = "none" | "environment" | "command-line";
export type GraphicsWarning = "invalid-config" | "unreadable-config" | "unsupported-version";
export interface GraphicsSettingsState {
  hardwareAcceleration: boolean;
  startupEnabled: boolean;
  override: GraphicsOverride;
  restartRequired: boolean;
  writable: boolean;
  warning: GraphicsWarning | null;
}
export interface GraphicsBootstrap {
  state: GraphicsSettingsState;
  shouldDisable: boolean;
  configPath: string;
}

type Stored = { version: 1; hardwareAcceleration: boolean; [key: string]: unknown };

function parseStored(path: string): { value: Stored | null; warning: GraphicsWarning | null } {
  if (!existsSync(path)) return { value: null, warning: null };
  try {
    const parsed = JSON.parse(readFileSync(path, "utf8")) as Record<string, unknown>;
    if (parsed.version !== 1) return { value: null, warning: "unsupported-version" };
    if (typeof parsed.hardwareAcceleration !== "boolean") return { value: null, warning: "invalid-config" };
    return { value: parsed as Stored, warning: null };
  } catch {
    return { value: null, warning: "unreadable-config" };
  }
}

export function loadGraphicsBootstrap(dataHome: string, env: NodeJS.ProcessEnv, argv: readonly string[]): GraphicsBootstrap {
  // `dataHome/desktop-shell` is Electron's userData profile (set by
  // claimShellInstance), so keep the preference directly in that profile.
  const configPath = join(dataHome, "graphics.json");
  const parsed = parseStored(configPath);
  const hardwareAcceleration = parsed.value?.hardwareAcceleration ?? true;
  const environmentOverride = env.REASONIX_DISABLE_GPU === "1";
  const commandLineOverride = argv.includes("--disable-gpu");
  const override: GraphicsOverride = environmentOverride ? "environment" : commandLineOverride ? "command-line" : "none";
  const startupEnabled = override === "none" ? hardwareAcceleration : false;
  return {
    configPath,
    shouldDisable: !startupEnabled,
    state: {
      hardwareAcceleration,
      startupEnabled,
      override,
      restartRequired: override === "none" && hardwareAcceleration !== startupEnabled,
      writable: parsed.warning !== "unsupported-version",
      warning: parsed.warning,
    },
  };
}

export class GraphicsSettingsStore {
  private stored: Stored | null;
  constructor(private readonly path: string, bootstrap: GraphicsBootstrap) {
    this.stored = parseStored(path).value ?? { version: 1, hardwareAcceleration: bootstrap.state.hardwareAcceleration };
    this.state = bootstrap.state;
  }
  private state: GraphicsSettingsState;
  private writeQueue: Promise<void> = Promise.resolve();
  get current(): GraphicsSettingsState { return this.state; }
  async setHardwareAcceleration(enabled: boolean): Promise<GraphicsSettingsState> {
    if (typeof enabled !== "boolean") throw new Error("hardwareAcceleration must be boolean");
    if (!this.state.writable) throw new Error("graphics settings are read-only");
    const next: Stored = { ...this.stored, version: 1, hardwareAcceleration: enabled };
    const write = this.writeQueue.catch(() => undefined).then(() => {
      mkdirSync(dirname(this.path), { recursive: true });
      const existing = parseStored(this.path);
    if (existing.warning) {
      let backup = `${this.path}.invalid`;
      let suffix = 1;
      while (existsSync(backup)) backup = `${this.path}.invalid.${suffix++}`;
      renameSync(this.path, backup);
    }
      const temp = `${this.path}.tmp`;
      writeFileSync(temp, `${JSON.stringify(next)}\n`, "utf8");
      renameSync(temp, this.path);
    });
    this.writeQueue = write.catch(() => undefined);
    await write;
    this.stored = next;
    this.state = { ...this.state, hardwareAcceleration: enabled, restartRequired: this.state.override === "none" && enabled !== this.state.startupEnabled, warning: null };
    return this.state;
  }
}
