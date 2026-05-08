/**
 * First-run / re-configure wizard.
 *
 * Walks a new user through: language → API key → preset pick → MCP server
 * pick → per-server args → save. Saved output lives in
 * `~/.reasonix/config.json` so the next `reasonix chat` starts with
 * everything already wired.
 */

import { mkdirSync, statSync } from "node:fs";
import { Box, Text, useApp, useInput } from "ink";
import TextInput from "ink-text-input";
// biome-ignore lint/style/useImportType: JSX (jsx: "react") needs React as a value at runtime
import React, { useEffect, useState } from "react";
import {
  type PresetName,
  type ReasonixConfig,
  defaultConfigPath,
  isPlausibleKey,
  readConfig,
  redactKey,
  writeConfig,
} from "../../config.js";
import {
  detectSystemLanguage,
  getLanguage,
  getSupportedLanguages,
  notifyLanguageChange,
  onLanguageChange,
  setLanguage,
  t,
} from "../../i18n/index.js";
import type { LanguageCode } from "../../i18n/types.js";
import { type CatalogEntry, MCP_CATALOG } from "../../mcp/catalog.js";
import { MultiSelect, type SelectItem, SingleSelect } from "./Select.js";
import { PRESET_DESCRIPTIONS } from "./presets.js";

export interface WizardProps {
  /** Called once the config has been saved. */
  onComplete: (cfg: ReasonixConfig) => void;
  /** Called if the user presses Esc to abort. */
  onCancel?: () => void;
  /** Skip the API-key step if a key already exists (env or config). */
  existingApiKey?: string;
  /** Pre-fill selections when re-running (reconfigure flow). */
  initial?: {
    preset?: PresetName;
    mcp?: string[];
  };
}

type Step = "language" | "apiKey" | "preset" | "mcp" | "mcpArgs" | "review" | "saved";

interface WizardData {
  language: LanguageCode;
  apiKey: string;
  preset: PresetName;
  selectedCatalog: string[];
  catalogArgs: Record<string, string>;
}

const CATALOG_BY_NAME = new Map(MCP_CATALOG.map((e) => [e.name, e]));

const LANGUAGE_LABELS: Record<LanguageCode, string> = {
  EN: "English",
  "zh-CN": "简体中文",
};

export function Wizard({ onComplete, onCancel, existingApiKey, initial }: WizardProps) {
  const { exit } = useApp();
  const [, setLanguageVersion] = useState(0);
  useEffect(() => onLanguageChange(() => setLanguageVersion((v) => v + 1)), []);

  const [step, setStep] = useState<Step>("language");
  const [data, setData] = useState<WizardData>({
    language: getLanguage(),
    apiKey: existingApiKey ?? "",
    preset: initial?.preset ?? "auto",
    selectedCatalog: deriveInitialCatalog(initial?.mcp ?? []),
    catalogArgs: {},
  });
  const [error, setError] = useState<string | null>(null);

  useInput((_input, key) => {
    if (key.escape && step !== "saved" && onCancel) onCancel();
  });

  if (step === "language") {
    return (
      <LanguageStep
        initialValue={data.language}
        onSubmit={(lang) => {
          setLanguage(lang);
          notifyLanguageChange();
          setData((d) => ({ ...d, language: lang }));
          setStep(existingApiKey ? "preset" : "apiKey");
        }}
      />
    );
  }

  if (step === "apiKey") {
    return (
      <ApiKeyStep
        onSubmit={(key) => {
          setData((d) => ({ ...d, apiKey: key }));
          setError(null);
          setStep("preset");
        }}
        error={error}
        onError={setError}
      />
    );
  }

  if (step === "preset") {
    return (
      <StepFrame title={t("wizard.presetTitle")} step={1} total={3}>
        <SingleSelect<PresetName>
          items={presetItems()}
          initialValue={data.preset}
          onSubmit={(preset) => {
            setData((d) => ({ ...d, preset }));
            setStep("mcp");
          }}
        />
        <Box marginTop={1}>
          <Text dimColor>{t("wizard.selectFooter")}</Text>
        </Box>
      </StepFrame>
    );
  }

  if (step === "mcp") {
    return (
      <StepFrame title={t("wizard.mcpTitle")} step={2} total={3}>
        <MultiSelect
          items={mcpItems()}
          initialSelected={data.selectedCatalog}
          onSubmit={(selected) => {
            setData((d) => ({ ...d, selectedCatalog: selected }));
            const needsArgs = selected.some((name) => CATALOG_BY_NAME.get(name)?.userArgs);
            setStep(needsArgs ? "mcpArgs" : "review");
          }}
          footer={t("wizard.mcpFooterMulti")}
        />
      </StepFrame>
    );
  }

  if (step === "mcpArgs") {
    const pending = data.selectedCatalog.filter((name) => {
      const entry = CATALOG_BY_NAME.get(name);
      return entry?.userArgs && !data.catalogArgs[name];
    });
    if (pending.length === 0) {
      setStep("review");
      return null;
    }
    const currentName = pending[0]!;
    const entry = CATALOG_BY_NAME.get(currentName)!;
    return (
      <McpArgsStep
        entry={entry}
        error={error}
        onSubmit={(value) => {
          setData((d) => ({
            ...d,
            catalogArgs: { ...d.catalogArgs, [currentName]: value },
          }));
          setError(null);
        }}
        onError={setError}
      />
    );
  }

  if (step === "review") {
    const specs = data.selectedCatalog.map((name) => buildSpec(name, data.catalogArgs));
    return (
      <StepFrame title={t("wizard.reviewTitle")} step={3} total={3}>
        <Box flexDirection="column">
          <SummaryLine
            label={t("wizard.reviewLabelLanguage")}
            value={LANGUAGE_LABELS[data.language]}
          />
          <SummaryLine label={t("wizard.reviewLabelApiKey")} value={redactKey(data.apiKey)} />
          <SummaryLine label={t("wizard.reviewLabelPreset")} value={data.preset} />
          <SummaryLine
            label={t("wizard.reviewLabelMcp")}
            value={
              specs.length === 0
                ? t("wizard.reviewMcpNone")
                : t("wizard.reviewMcpServers", { count: specs.length })
            }
          />
          {specs.map((spec, i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: review-only render, order fixed
            <Box key={i} paddingLeft={14}>
              <Text dimColor>· {spec}</Text>
            </Box>
          ))}
          <Box marginTop={1}>
            <Text>{t("wizard.reviewSavesTo", { path: defaultConfigPath() })}</Text>
          </Box>
          {error ? (
            <Box marginTop={1}>
              <Text color="red">{error}</Text>
            </Box>
          ) : null}
          <Box marginTop={1}>
            <Text dimColor>{t("wizard.reviewFooter")}</Text>
          </Box>
        </Box>
        <ReviewConfirm
          onConfirm={() => {
            try {
              const specsNow = data.selectedCatalog.map((name) =>
                buildSpec(name, data.catalogArgs),
              );
              const prev = readConfig();
              const next: ReasonixConfig = {
                ...prev,
                apiKey: data.apiKey,
                preset: data.preset,
                mcp: specsNow,
                setupCompleted: true,
              };
              writeConfig(next);
              setStep("saved");
              onComplete(next);
            } catch (e) {
              setError(t("wizard.reviewSaveError", { message: (e as Error).message }));
            }
          }}
        />
      </StepFrame>
    );
  }

  return (
    <Box flexDirection="column" borderStyle="round" borderColor="green" paddingX={1}>
      <Text bold color="green">
        {t("wizard.savedTitle")}
      </Text>
      <Box marginTop={1}>
        <Text>{t("ui.welcome")}</Text>
      </Box>
      <Box marginTop={1}>
        <Text dimColor>{t("wizard.savedFooter")}</Text>
      </Box>
      <ExitOnEnter onExit={exit} />
    </Box>
  );
}

// ---------- step components ----------

function LanguageStep({
  initialValue,
  onSubmit,
}: {
  initialValue: LanguageCode;
  onSubmit: (lang: LanguageCode) => void;
}) {
  const items: SelectItem<LanguageCode>[] = getSupportedLanguages().map((code) => ({
    value: code,
    label: LANGUAGE_LABELS[code],
    hint: code === detectSystemLanguage() ? "(detected)" : undefined,
  }));
  return (
    <Box flexDirection="column" borderStyle="round" borderColor="cyan" paddingX={1}>
      <Text bold color="cyan">
        {t("wizard.languageTitle")}
      </Text>
      <Box marginTop={1}>
        <Text dimColor>{t("wizard.languageSubtitle")}</Text>
      </Box>
      <Box marginTop={1}>
        <SingleSelect<LanguageCode>
          items={items}
          initialValue={initialValue}
          onSubmit={onSubmit}
          footer={t("wizard.selectFooter")}
        />
      </Box>
    </Box>
  );
}

function ApiKeyStep({
  onSubmit,
  error,
  onError,
}: {
  onSubmit: (key: string) => void;
  error: string | null;
  onError: (e: string | null) => void;
}) {
  const [value, setValue] = useState("");
  return (
    <Box flexDirection="column" borderStyle="round" borderColor="cyan" paddingX={1}>
      <Text bold color="cyan">
        {t("wizard.welcomeTitle")}
      </Text>
      <Box marginTop={1}>
        <Text>{t("wizard.apiKeyPrompt")}</Text>
      </Box>
      <Text dimColor>{t("wizard.apiKeyGetOne")}</Text>
      <Text dimColor>{t("wizard.apiKeySavedLocally", { path: defaultConfigPath() })}</Text>
      <Box marginTop={1}>
        <Text bold color="cyan">
          {t("wizard.apiKeyInputLabel")}
        </Text>
        <TextInput
          value={value}
          onChange={setValue}
          onSubmit={(raw) => {
            const trimmed = raw.trim();
            if (!isPlausibleKey(trimmed)) {
              onError(t("wizard.apiKeyInvalid"));
              setValue("");
              return;
            }
            onSubmit(trimmed);
          }}
          mask="•"
          placeholder="sk-..."
        />
      </Box>
      {error ? (
        <Box marginTop={1}>
          <Text color="red">{error}</Text>
        </Box>
      ) : value ? (
        <Box marginTop={1}>
          <Text dimColor>{t("wizard.apiKeyPreview", { redacted: redactKey(value) })}</Text>
        </Box>
      ) : null}
    </Box>
  );
}

function McpArgsStep({
  entry,
  error,
  onSubmit,
  onError,
}: {
  entry: CatalogEntry;
  error: string | null;
  onSubmit: (value: string) => void;
  onError: (e: string | null) => void;
}) {
  const [value, setValue] = useState("");
  const [pendingCreate, setPendingCreate] = useState<string | null>(null);

  useInput((input, key) => {
    if (!pendingCreate) return;
    const ch = input.toLowerCase();
    if (ch === "y" || key.return) {
      try {
        mkdirSync(pendingCreate, { recursive: true });
        const created = pendingCreate;
        setPendingCreate(null);
        setValue("");
        onError(null);
        onSubmit(created);
      } catch (e) {
        onError(
          t("wizard.mcpArgsDirCreateFailed", {
            path: pendingCreate,
            message: (e as Error).message,
          }),
        );
        setPendingCreate(null);
      }
    } else if (ch === "n" || key.escape) {
      setPendingCreate(null);
      onError(null);
    }
  });

  if (pendingCreate) {
    return (
      <StepFrame title={t("wizard.mcpArgsTitle", { name: entry.name })} step={2} total={3}>
        <Box flexDirection="column">
          <Text>{t("wizard.mcpArgsDirMissing", { path: pendingCreate })}</Text>
          <Box marginTop={1}>
            <Text dimColor>{t("wizard.mcpArgsDirCreateHint")}</Text>
          </Box>
          {error ? (
            <Box marginTop={1}>
              <Text color="red">{error}</Text>
            </Box>
          ) : null}
        </Box>
      </StepFrame>
    );
  }

  return (
    <StepFrame title={t("wizard.mcpArgsTitle", { name: entry.name })} step={2} total={3}>
      <Box flexDirection="column">
        <Text>{entry.summary}</Text>
        {entry.note ? (
          <Box marginTop={1}>
            <Text dimColor>{entry.note}</Text>
          </Box>
        ) : null}
        <Box marginTop={1}>
          <Text>{t("wizard.mcpArgsRequiredParam")}</Text>
          <Text bold>{entry.userArgs}</Text>
        </Box>
        <Box marginTop={1}>
          <Text bold color="cyan">
            {entry.userArgs}
            {" › "}
          </Text>
          <TextInput
            value={value}
            onChange={setValue}
            onSubmit={(raw) => {
              const trimmed = raw.trim();
              if (!trimmed) {
                onError(t("wizard.mcpArgsEmpty", { name: entry.name }));
                return;
              }
              if (entry.name === "filesystem") {
                const check = checkFilesystemPath(trimmed);
                if (check.kind === "missing") {
                  setPendingCreate(trimmed);
                  return;
                }
                if (check.kind === "not-a-dir") {
                  onError(t("wizard.mcpArgsNotADir", { path: trimmed }));
                  return;
                }
              }
              onSubmit(trimmed);
              setValue("");
            }}
            placeholder={placeholderFor(entry)}
          />
        </Box>
        {error ? (
          <Box marginTop={1}>
            <Text color="red">{error}</Text>
          </Box>
        ) : null}
      </Box>
    </StepFrame>
  );
}

function checkFilesystemPath(p: string): { kind: "ok" | "missing" | "not-a-dir" } {
  try {
    return { kind: statSync(p).isDirectory() ? "ok" : "not-a-dir" };
  } catch {
    return { kind: "missing" };
  }
}

function ReviewConfirm({ onConfirm }: { onConfirm: () => void }) {
  useInput((_i, key) => {
    if (key.return) onConfirm();
  });
  return null;
}

function ExitOnEnter({ onExit }: { onExit: () => void }) {
  useInput((_i, key) => {
    if (key.return) onExit();
  });
  return null;
}

function StepFrame({
  title,
  step,
  total,
  children,
}: {
  title: string;
  step: number;
  total: number;
  children: React.ReactNode;
}) {
  return (
    <Box flexDirection="column" borderStyle="round" borderColor="cyan" paddingX={1}>
      <Box>
        <Text dimColor>{t("wizard.stepCounter", { step, total })}</Text>
        <Text bold color="cyan">
          {title}
        </Text>
      </Box>
      <Box marginTop={1} flexDirection="column">
        {children}
      </Box>
    </Box>
  );
}

function SummaryLine({ label, value }: { label: string; value: string }) {
  return (
    <Box>
      <Text>{label.padEnd(12)}</Text>
      <Text bold>{value}</Text>
    </Box>
  );
}

function presetItems(): SelectItem<PresetName>[] {
  return (["auto", "flash", "pro"] as const).map((name) => ({
    value: name as PresetName,
    label: `${name} — ${PRESET_DESCRIPTIONS[name].headline}`,
    hint: PRESET_DESCRIPTIONS[name].cost,
  }));
}

function mcpItems(): SelectItem<string>[] {
  return MCP_CATALOG.map((entry) => {
    const hintParts: string[] = [entry.summary];
    if (entry.userArgs) hintParts.push(t("wizard.mcpUserArgsHint", { arg: entry.userArgs }));
    if (entry.note) hintParts.push(entry.note);
    return {
      value: entry.name,
      label: entry.name,
      hint: hintParts.join(" · "),
    };
  });
}

function placeholderFor(entry: CatalogEntry): string {
  if (entry.name === "filesystem") return "e.g. /tmp/reasonix-sandbox";
  if (entry.name === "sqlite") return "e.g. ./notes.sqlite";
  return entry.userArgs ?? "";
}

function deriveInitialCatalog(existingSpecs: string[]): string[] {
  const packageToName = new Map(MCP_CATALOG.map((e) => [e.package, e.name]));
  const out: string[] = [];
  for (const spec of existingSpecs) {
    for (const [pkg, name] of packageToName) {
      if (spec.includes(pkg)) {
        out.push(name);
        break;
      }
    }
  }
  return out;
}

/**
 * Build the `--mcp` spec string for a catalog entry. Same format
 * `mcpCommandFor` produces for `reasonix mcp list`, minus the leading
 * `--mcp "..."` wrapper — we store the inner spec directly.
 */
export function buildSpec(name: string, argsByName: Record<string, string>): string {
  const entry = CATALOG_BY_NAME.get(name);
  if (!entry) return name;
  const userArg = entry.userArgs ? argsByName[name] : undefined;
  const tail = userArg ? ` ${quoteIfNeeded(userArg)}` : "";
  return `${entry.name}=npx -y ${entry.package}${tail}`;
}

function quoteIfNeeded(s: string): string {
  // Escape backslashes BEFORE quotes — otherwise a trailing `\` in the
  // input would consume the closing quote when a downstream parser
  // un-escapes the output (CodeQL js/incomplete-sanitization).
  return /\s|"/.test(s) ? `"${s.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"` : s;
}
