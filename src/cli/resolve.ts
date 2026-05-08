/** Precedence: per-setting flag > --preset > config.preset > "auto" defaults. */

import { type PresetName, type ReasonixConfig, readConfig } from "../config.js";
import { resolvePreset } from "./ui/presets.js";

export interface ResolvedDefaults {
  model: string;
  reasoningEffort: "high" | "max";
  mcp: string[];
  session: string | undefined;
}

export interface RawCliFlags {
  model?: string;
  mcp?: string[];
  /** Commander's `--no-session` surfaces as `false`; `--session X` as a string. */
  session?: string | false;
  /** `--preset <name>`. */
  preset?: string;
  /** When true, ignore config entirely (power-user escape hatch). */
  noConfig?: boolean;
}

export function resolveDefaults(flags: RawCliFlags): ResolvedDefaults {
  const cfg: ReasonixConfig = flags.noConfig ? {} : readConfig();
  const preset = pickPreset(flags.preset, cfg.preset);
  const presetSettings = resolvePreset(preset);

  const model = flags.model ?? presetSettings.model;
  const reasoningEffort = presetSettings.reasoningEffort;

  // `--mcp` accumulator is [] when absent. Treat empty from flags as
  // "user didn't pass" → fall through to config. Users who explicitly
  // want zero MCP servers can pass `--no-config` or edit the file.
  const mcp = flags.mcp && flags.mcp.length > 0 ? flags.mcp : (cfg.mcp ?? []);

  const session = resolveSession(flags.session, cfg.session);

  return { model, reasoningEffort, mcp, session };
}

function pickPreset(
  flagPreset: string | undefined,
  configPreset: PresetName | undefined,
): PresetName {
  if (flagPreset && isPresetName(flagPreset)) return flagPreset;
  if (configPreset) return configPreset;
  return "auto";
}

function isPresetName(s: string): s is PresetName {
  return (
    s === "auto" ||
    s === "flash" ||
    s === "pro" ||
    // Legacy names — kept callable so old `--preset smart` invocations
    // and stale config.json entries don't error out.
    s === "fast" ||
    s === "smart" ||
    s === "max"
  );
}

function resolveSession(
  flag: string | false | undefined,
  configSession: string | null | undefined,
): string | undefined {
  if (flag === false) return undefined; // --no-session
  if (typeof flag === "string" && flag.length > 0) return flag;
  if (configSession === null) return undefined; // config opted out
  if (typeof configSession === "string" && configSession.length > 0) return configSession;
  return "default";
}

export function resolveContinueFlag(
  flag: boolean | undefined,
  fallbackSession: string | undefined,
  getLatestSession: () => { name: string } | undefined,
  warn: (msg: string) => void = () => {},
): { session: string | undefined; forceResume: boolean } {
  if (!flag) return { session: fallbackSession, forceResume: false };
  const latest = getLatestSession();
  if (!latest) {
    warn("▸ -c/--continue: no saved sessions yet — starting a fresh one.");
    return { session: fallbackSession, forceResume: false };
  }
  return { session: latest.name, forceResume: true };
}
