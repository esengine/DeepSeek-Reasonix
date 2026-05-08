/** Library reads only DEEPSEEK_API_KEY from env; the CLI bridges config.json → env var. */

import { chmodSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import type { LanguageCode } from "./i18n/types.js";
import {
  type IndexUserConfig,
  type ResolvedIndexConfig,
  resolveIndexConfig,
} from "./index/config.js";

/** Legacy `fast|smart|max` kept for back-compat with existing config.json files. */
export type PresetName = "auto" | "flash" | "pro" | "fast" | "smart" | "max";

/** Single trust dial: review queues edits + gates shell; auto applies + gates shell; yolo skips both gates. */
export type EditMode = "review" | "auto" | "yolo";

export type ReasoningEffort = "high" | "max";

export type EmbeddingProvider = "ollama" | "openai-compat";

export interface OllamaEmbeddingUserConfig {
  baseUrl?: string;
  model?: string;
}

export interface OpenAICompatEmbeddingUserConfig {
  baseUrl?: string;
  apiKey?: string;
  model?: string;
  extraBody?: Record<string, unknown>;
}

export interface SemanticEmbeddingUserConfig {
  provider?: EmbeddingProvider;
  ollama?: OllamaEmbeddingUserConfig;
  openaiCompat?: OpenAICompatEmbeddingUserConfig;
}

export interface ResolvedOllamaEmbeddingConfig {
  provider: "ollama";
  baseUrl: string;
  model: string;
  timeoutMs: number;
}

export interface ResolvedOpenAICompatEmbeddingConfig {
  provider: "openai-compat";
  baseUrl: string;
  apiKey: string;
  model: string;
  extraBody: Record<string, unknown>;
  timeoutMs: number;
}

export type ResolvedEmbeddingConfig =
  | ResolvedOllamaEmbeddingConfig
  | ResolvedOpenAICompatEmbeddingConfig;

export interface SemanticEmbeddingConfigView {
  provider: EmbeddingProvider;
  ollama: {
    baseUrl: string;
    model: string;
  };
  openaiCompat: {
    baseUrl: string;
    apiKey: string;
    apiKeySet: boolean;
    model: string;
    extraBody: Record<string, unknown>;
  };
}

export interface ReasonixConfig {
  apiKey?: string;
  baseUrl?: string;
  lang?: LanguageCode;
  preset?: PresetName;
  editMode?: EditMode;
  editModeHintShown?: boolean;
  reasoningEffort?: ReasoningEffort;
  /** Stored as `--mcp`-format strings so one parser handles both flag and config. */
  mcp?: string[];
  /** Names of servers in `mcp` to skip on bridge — see `/mcp disable <name>`. */
  mcpDisabled?: string[];
  session?: string | null;
  setupCompleted?: boolean;
  search?: boolean;
  /** Web search engine backend: "mojeek" (default, scrapes Mojeek) or "searxng" (self-hosted SearXNG). */
  webSearchEngine?: "mojeek" | "searxng";
  /** Base URL for SearXNG instance (default http://localhost:8080). */
  webSearchEndpoint?: string;
  projects?: {
    [absoluteRootDir: string]: {
      shellAllowed?: string[];
    };
  };
  index?: IndexUserConfig;
  semantic?: SemanticEmbeddingUserConfig;
}

const DEFAULT_OLLAMA_URL = "http://localhost:11434";
const DEFAULT_EMBED_MODEL = "nomic-embed-text";
const DEFAULT_TIMEOUT_MS = 30_000;

export function defaultConfigPath(): string {
  return join(homedir(), ".reasonix", "config.json");
}

export function readConfig(path: string = defaultConfigPath()): ReasonixConfig {
  try {
    const raw = readFileSync(path, "utf8");
    const parsed = JSON.parse(raw);
    if (parsed && typeof parsed === "object") return parsed as ReasonixConfig;
  } catch {
    /* missing or malformed → empty config */
  }
  return {};
}

export function writeConfig(cfg: ReasonixConfig, path: string = defaultConfigPath()): void {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, JSON.stringify(cfg, null, 2), "utf8");
  try {
    chmodSync(path, 0o600);
  } catch {
    /* ignore on platforms without chmod */
  }
}

/** Resolve the language from config file. */
export function loadLanguage(path: string = defaultConfigPath()): LanguageCode | undefined {
  return readConfig(path).lang;
}

/** Persist the language so it survives a relaunch. */
export function saveLanguage(lang: LanguageCode, path: string = defaultConfigPath()): void {
  const cfg = readConfig(path);
  cfg.lang = lang;
  writeConfig(cfg, path);
}

/** Resolve the API key from env var first, then the config file. */
export function loadApiKey(path: string = defaultConfigPath()): string | undefined {
  if (process.env.DEEPSEEK_API_KEY) return process.env.DEEPSEEK_API_KEY;
  return readConfig(path).apiKey;
}

export function searchEnabled(path: string = defaultConfigPath()): boolean {
  const env = process.env.REASONIX_SEARCH;
  if (env === "off" || env === "false" || env === "0") return false;
  const cfg = readConfig(path).search;
  if (cfg === false) return false;
  return true;
}

export function webSearchEngine(path: string = defaultConfigPath()): "mojeek" | "searxng" {
  const cfg = readConfig(path).webSearchEngine;
  if (cfg === "searxng") return "searxng";
  return "mojeek";
}

export function webSearchEndpoint(path: string = defaultConfigPath()): string {
  const cfg = readConfig(path).webSearchEndpoint;
  if (cfg && typeof cfg === "string") return cfg;
  return "http://localhost:8080";
}

export function saveApiKey(key: string, path: string = defaultConfigPath()): void {
  const cfg = readConfig(path);
  cfg.apiKey = key.trim();
  writeConfig(cfg, path);
}

/** Windows: case-insensitive — NTFS treats `F:\Foo` and `f:\foo` as one directory (#402). */
function findProjectKey(cfg: ReasonixConfig, rootDir: string): string | undefined {
  const projects = cfg.projects;
  if (!projects) return undefined;
  if (Object.hasOwn(projects, rootDir)) return rootDir;
  if (process.platform !== "win32") return undefined;
  const lower = rootDir.toLowerCase();
  for (const k of Object.keys(projects)) {
    if (k.toLowerCase() === lower) return k;
  }
  return undefined;
}

export function loadProjectShellAllowed(
  rootDir: string,
  path: string = defaultConfigPath(),
): string[] {
  const cfg = readConfig(path);
  const key = findProjectKey(cfg, rootDir);
  if (key === undefined) return [];
  return cfg.projects?.[key]?.shellAllowed ?? [];
}

export function addProjectShellAllowed(
  rootDir: string,
  prefix: string,
  path: string = defaultConfigPath(),
): void {
  const trimmed = prefix.trim();
  if (!trimmed) return;
  const cfg = readConfig(path);
  if (!cfg.projects) cfg.projects = {};
  const key = findProjectKey(cfg, rootDir) ?? rootDir;
  if (!cfg.projects[key]) cfg.projects[key] = {};
  const existing = cfg.projects[key].shellAllowed ?? [];
  if (existing.includes(trimmed)) return;
  cfg.projects[key].shellAllowed = [...existing, trimmed];
  writeConfig(cfg, path);
}

/** Match is exact after trim — NOT prefix-match: removing `git` MUST NOT drop `git push origin main`. */
export function removeProjectShellAllowed(
  rootDir: string,
  prefix: string,
  path: string = defaultConfigPath(),
): boolean {
  const trimmed = prefix.trim();
  if (!trimmed) return false;
  const cfg = readConfig(path);
  const key = findProjectKey(cfg, rootDir);
  if (key === undefined) return false;
  const existing = cfg.projects?.[key]?.shellAllowed ?? [];
  if (!existing.includes(trimmed)) return false;
  const next = existing.filter((p) => p !== trimmed);
  if (!cfg.projects) cfg.projects = {};
  if (!cfg.projects[key]) cfg.projects[key] = {};
  cfg.projects[key].shellAllowed = next;
  writeConfig(cfg, path);
  return true;
}

export function clearProjectShellAllowed(
  rootDir: string,
  path: string = defaultConfigPath(),
): number {
  const cfg = readConfig(path);
  const key = findProjectKey(cfg, rootDir);
  if (key === undefined) return 0;
  const existing = cfg.projects?.[key]?.shellAllowed ?? [];
  if (existing.length === 0) return 0;
  if (!cfg.projects) cfg.projects = {};
  if (!cfg.projects[key]) cfg.projects[key] = {};
  cfg.projects[key].shellAllowed = [];
  writeConfig(cfg, path);
  return existing.length;
}

/** Unknown values fall back to "review" so hand-edited bad config gets the safe default. */
export function loadEditMode(path: string = defaultConfigPath()): EditMode {
  const v = readConfig(path).editMode;
  return v === "auto" ? "auto" : "review";
}

/** Persist the edit mode so `/mode auto` survives a relaunch. */
export function saveEditMode(mode: EditMode, path: string = defaultConfigPath()): void {
  const cfg = readConfig(path);
  cfg.editMode = mode;
  writeConfig(cfg, path);
}

/** True when the onboarding tip for the review/AUTO gate has been shown. */
export function editModeHintShown(path: string = defaultConfigPath()): boolean {
  return readConfig(path).editModeHintShown === true;
}

/** Unknown / missing fall back to "max" so hand-edited bad config can't silently override the default. */
export function loadReasoningEffort(path: string = defaultConfigPath()): ReasoningEffort {
  const v = readConfig(path).reasoningEffort;
  return v === "high" ? "high" : "max";
}

/** Persist the reasoning_effort cap so `/effort high` survives a relaunch. */
export function saveReasoningEffort(
  effort: ReasoningEffort,
  path: string = defaultConfigPath(),
): void {
  const cfg = readConfig(path);
  cfg.reasoningEffort = effort;
  writeConfig(cfg, path);
}

export function loadIndexUserConfig(path: string = defaultConfigPath()): IndexUserConfig {
  return readConfig(path).index ?? {};
}

export function loadIndexConfig(path: string = defaultConfigPath()): ResolvedIndexConfig {
  return resolveIndexConfig(readConfig(path).index);
}

export function saveIndexConfig(user: IndexUserConfig, path: string = defaultConfigPath()): void {
  const cfg = readConfig(path);
  cfg.index = user;
  writeConfig(cfg, path);
}

export function loadSemanticEmbeddingUserConfig(
  path: string = defaultConfigPath(),
): SemanticEmbeddingUserConfig {
  return normalizeSemanticEmbeddingUserConfig(readConfig(path).semantic);
}

export function saveSemanticEmbeddingConfig(
  user: SemanticEmbeddingUserConfig,
  path: string = defaultConfigPath(),
): void {
  const cfg = readConfig(path);
  cfg.semantic = normalizeSemanticEmbeddingUserConfig(user);
  writeConfig(cfg, path);
}

export function resolveSemanticEmbeddingConfig(
  path: string = defaultConfigPath(),
): ResolvedEmbeddingConfig {
  const user = loadSemanticEmbeddingUserConfig(path);
  const provider = user.provider ?? "ollama";
  if (provider === "openai-compat") {
    const baseUrl = user.openaiCompat?.baseUrl?.trim() ?? "";
    const apiKey = user.openaiCompat?.apiKey?.trim() ?? "";
    const model = user.openaiCompat?.model?.trim() ?? "";
    if (!baseUrl) throw new Error("OpenAI-compatible embeddings require an API URL.");
    requireValidUrl(baseUrl, "OpenAI-compatible API URL");
    if (!apiKey) throw new Error("OpenAI-compatible embeddings require an API key.");
    if (!model) throw new Error("OpenAI-compatible embeddings require a model.");
    return {
      provider,
      baseUrl,
      apiKey,
      model,
      extraBody: normalizeExtraBody(user.openaiCompat?.extraBody),
      timeoutMs: DEFAULT_TIMEOUT_MS,
    };
  }
  return {
    provider: "ollama",
    baseUrl: user.ollama?.baseUrl?.trim() || process.env.OLLAMA_URL || DEFAULT_OLLAMA_URL,
    model: user.ollama?.model?.trim() || process.env.REASONIX_EMBED_MODEL || DEFAULT_EMBED_MODEL,
    timeoutMs: DEFAULT_TIMEOUT_MS,
  };
}

export function redactSemanticEmbeddingConfig(
  user: SemanticEmbeddingUserConfig,
): SemanticEmbeddingConfigView {
  const normalized = normalizeSemanticEmbeddingUserConfig(user);
  return {
    provider: normalized.provider ?? "ollama",
    ollama: {
      baseUrl: normalized.ollama?.baseUrl?.trim() || process.env.OLLAMA_URL || DEFAULT_OLLAMA_URL,
      model:
        normalized.ollama?.model?.trim() || process.env.REASONIX_EMBED_MODEL || DEFAULT_EMBED_MODEL,
    },
    openaiCompat: {
      baseUrl: normalized.openaiCompat?.baseUrl?.trim() ?? "",
      apiKey: normalized.openaiCompat?.apiKey ? redactKey(normalized.openaiCompat.apiKey) : "",
      apiKeySet: Boolean(normalized.openaiCompat?.apiKey?.trim()),
      model: normalized.openaiCompat?.model?.trim() ?? "",
      extraBody: normalizeExtraBody(normalized.openaiCompat?.extraBody),
    },
  };
}

/** Mark the onboarding tip as shown so subsequent launches skip it. */
export function markEditModeHintShown(path: string = defaultConfigPath()): void {
  const cfg = readConfig(path);
  if (cfg.editModeHintShown === true) return;
  cfg.editModeHintShown = true;
  writeConfig(cfg, path);
}

export function isPlausibleKey(key: string): boolean {
  const trimmed = key.trim();
  return /^sk-[A-Za-z0-9_-]{16,}$/.test(trimmed);
}

/** Mask a key for display: `sk-abcd...wxyz`. */
export function redactKey(key: string): string {
  if (!key) return "";
  if (key.length <= 12) return "****";
  return `${key.slice(0, 6)}…${key.slice(-4)}`;
}

function normalizeSemanticEmbeddingUserConfig(
  cfg: SemanticEmbeddingUserConfig | undefined,
): SemanticEmbeddingUserConfig {
  return {
    provider: cfg?.provider === "openai-compat" ? "openai-compat" : "ollama",
    ollama: {
      baseUrl: normalizeOptionalString(cfg?.ollama?.baseUrl),
      model: normalizeOptionalString(cfg?.ollama?.model),
    },
    openaiCompat: {
      baseUrl: normalizeOptionalString(cfg?.openaiCompat?.baseUrl),
      apiKey: normalizeOptionalString(cfg?.openaiCompat?.apiKey),
      model: normalizeOptionalString(cfg?.openaiCompat?.model),
      extraBody: normalizeExtraBody(cfg?.openaiCompat?.extraBody),
    },
  };
}

function normalizeOptionalString(value: string | undefined): string | undefined {
  const trimmed = value?.trim();
  return trimmed ? trimmed : undefined;
}

function normalizeExtraBody(value: Record<string, unknown> | undefined): Record<string, unknown> {
  if (value === undefined) return {};
  if (!isPlainObject(value)) {
    throw new Error("Semantic embedding extraBody must be a JSON object.");
  }
  return { ...value };
}

function requireValidUrl(value: string, label: string): void {
  try {
    new URL(value);
  } catch {
    throw new Error(`${label} must be a valid URL.`);
  }
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return false;
  const proto = Object.getPrototypeOf(value);
  return proto === Object.prototype || proto === null;
}
