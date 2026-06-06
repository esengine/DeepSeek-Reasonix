import {
  DEFAULT_MODEL,
  type ReasoningEffort,
  type ReasonixConfig,
  isReasoningEffort,
  loadModel,
  loadReasoningEffort,
  normalizeMcpConfig,
  readConfig,
} from "../config.js";
import { loadDotMcpJson } from "../mcp/dot-mcp-json.js";
import { specToRaw } from "../mcp/spec.js";

export interface ResolvedDefaults {
  model: string;
  reasoningEffort: ReasoningEffort;
  mcp: string[];
  session: string | undefined;
  /** True when autoResumeSession:false is in effect — tells callers to skip the session picker. */
  forceNew?: boolean;
}

export interface RawCliFlags {
  model?: string;
  mcp?: string[];
  /** Commander's `--no-session` surfaces as `false`; `--session X` as a string. */
  session?: string | false;
  /** `--effort low|medium|high|max`. */
  effort?: string;
  /** When true, ignore config entirely (power-user escape hatch). */
  noConfig?: boolean;
}

export function resolveDefaults(flags: RawCliFlags): ResolvedDefaults {
  const cfg: ReasonixConfig = flags.noConfig ? {} : readConfig();
  const model =
    flags.model?.trim() || (flags.noConfig ? cfg.model?.trim() : loadModel()) || DEFAULT_MODEL;

  const flagEffort = flags.effort?.toLowerCase();
  const reasoningEffort: ReasoningEffort = isReasoningEffort(flagEffort)
    ? flagEffort
    : flags.noConfig
      ? "high"
      : loadReasoningEffort();

  const merged = flags.noConfig ? cfg : mergeDotMcpJson(cfg, process.cwd());

  const normalizedMcp = normalizeMcpConfig(
    merged,
    flags.mcp && flags.mcp.length > 0 ? flags.mcp : undefined,
  );
  const mcp = normalizedMcp.map(specToRaw);

  const session = resolveSession(flags.session, cfg.session, cfg.autoResumeSession);
  const forceNew =
    cfg.autoResumeSession === false && flags.session === undefined ? true : undefined;

  return { model, reasoningEffort, mcp, session, forceNew };
}

function mergeDotMcpJson(cfg: ReasonixConfig, projectRoot: string): ReasonixConfig {
  const project = loadDotMcpJson(projectRoot);
  if (!project) return cfg;
  return { ...cfg, mcpServers: { ...(cfg.mcpServers ?? {}), ...project } };
}

function resolveSession(
  flag: string | false | undefined,
  configSession: string | null | undefined,
  autoResume?: boolean,
): string | undefined {
  if (flag === false) return undefined;
  if (typeof flag === "string" && flag.length > 0) return flag;
  if (configSession === null) return undefined;
  if (typeof configSession === "string" && configSession.length > 0) return configSession;
  // When autoResumeSession is explicitly false, don't default to "default" session
  // so each launch starts fresh instead of resuming the previous conversation (#2238).
  if (autoResume === false) return undefined;
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

export function resolveBareCommandMode(
  cfg: Pick<ReasonixConfig, "setupCompleted">,
): "setup" | "code" {
  if (!cfg.setupCompleted) return "setup";
  return "code";
}
