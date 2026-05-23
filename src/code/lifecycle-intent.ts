export type LifecycleIntentRecommendation = "stay-light" | "suggest-plan" | "strict-candidate";

export type LifecycleIntentConfidence = "low" | "medium" | "high";

// Advisory corpus/eval helper only; do not wire this into auto-arm without a separate RFC.
export type LifecycleIntentSignal =
  | "cancel"
  | "dependency-change"
  | "explicit-strict"
  | "failed-verification"
  | "migration"
  | "multi-file"
  | "package-or-config"
  | "plan-revision"
  | "read-only"
  | "refactor"
  | "single-file"
  | "small-task";

export interface LifecycleIntentEvaluation {
  recommendation: LifecycleIntentRecommendation;
  confidence: LifecycleIntentConfidence;
  score: number;
  signals: LifecycleIntentSignal[];
  modelVisible: false;
  enforced: false;
}

const EXPLICIT_STRICT_PHRASES = [
  "/plan strict",
  "strict lifecycle",
  "strict rails",
  "强约束",
  "严格生命周期",
];

const READ_ONLY_PHRASES = [
  "explain",
  "inspect",
  "look at",
  "read",
  "summarize",
  "do not edit",
  "without modifying",
  "解释",
  "查看",
  "看看",
  "总结",
  "不要修改",
  "只读",
];

const NEGATED_MUTATION_PHRASES = ["do not edit", "without modifying", "不要修改", "不要改"];

const MUTATION_PHRASES = [
  "add",
  "change",
  "delete",
  "edit",
  "fix",
  "install",
  "migrate",
  "move",
  "refactor",
  "remove",
  "rename",
  "rewrite",
  "upgrade",
  "update",
  "修",
  "改",
  "安装",
  "迁移",
  "移动",
  "删除",
  "升级",
  "更新",
  "重构",
  "重命名",
];

const SMALL_TASK_PHRASES = [
  "typo",
  "spelling",
  "one-line",
  "small fix",
  "minor fix",
  "拼写",
  "错别字",
  "小修",
  "修一下",
];

const SINGLE_FILE_PHRASES = ["single file", "one file", "readme", "单文件", "一个文件", "一个拼写"];

const MULTI_FILE_PHRASES = [
  "across",
  "all references",
  "multiple files",
  "multi-file",
  "routing layer",
  "多文件",
  "所有引用",
  "跨模块",
  "跨文件",
];

const REFACTOR_PHRASES = [
  "api",
  "move",
  "parser",
  "refactor",
  "rename",
  "模块",
  "移动",
  "重构",
  "重命名",
];

const MIGRATION_PHRASES = ["migrate", "migration", "迁移"];

const DEPENDENCY_PHRASES = [
  "npm install",
  "package.json",
  "pnpm add",
  "pnpm-lock",
  "yarn add",
  "依赖",
  "安装依赖",
];

const PACKAGE_OR_CONFIG_PHRASES = [
  ".github/workflows",
  "github actions",
  "lockfile",
  "package.json",
  "pnpm-lock",
  "tsconfig",
  "workflow",
  "配置",
];

const FAILED_VERIFICATION_PHRASES = [
  "failed after",
  "failing test",
  "test failed",
  "tests failed",
  "测试失败",
  "验证失败",
];

const PLAN_REVISION_PHRASES = ["revise_plan", "revise plan", "replace the remaining", "修正计划"];

const CANCEL_PHRASES = ["cancel", "stop", "取消", "停止"];

export function evaluateEngineeringLifecycleIntent(input: string): LifecycleIntentEvaluation {
  const text = normalize(input);
  const signals = new Set<LifecycleIntentSignal>();

  if (!text) return result("stay-light", "low", 0, signals);

  const mutationIntent = hasMutationIntent(text);

  addSignalIf(signals, "explicit-strict", hasAny(text, EXPLICIT_STRICT_PHRASES));
  addSignalIf(signals, "read-only", hasAny(text, READ_ONLY_PHRASES) && !mutationIntent);
  addSignalIf(signals, "small-task", hasAny(text, SMALL_TASK_PHRASES));
  addSignalIf(signals, "single-file", hasSingleFileSignal(text));
  addSignalIf(signals, "multi-file", hasAny(text, MULTI_FILE_PHRASES));
  addSignalIf(signals, "refactor", hasAny(text, REFACTOR_PHRASES));
  addSignalIf(signals, "migration", hasAny(text, MIGRATION_PHRASES));
  addSignalIf(signals, "dependency-change", mutationIntent && hasAny(text, DEPENDENCY_PHRASES));
  addSignalIf(
    signals,
    "package-or-config",
    mutationIntent && hasAny(text, PACKAGE_OR_CONFIG_PHRASES),
  );
  addSignalIf(signals, "failed-verification", hasAny(text, FAILED_VERIFICATION_PHRASES));
  addSignalIf(signals, "plan-revision", hasAny(text, PLAN_REVISION_PHRASES));
  addSignalIf(signals, "cancel", hasAny(text, CANCEL_PHRASES));

  if (isCancelOnly(signals)) return result("stay-light", "high", 0, signals);

  const score = scoreSignals(signals);
  if (score >= 4) return result("strict-candidate", "high", score, signals);
  if (score >= 2) return result("suggest-plan", "medium", score, signals);

  if (signals.has("read-only") || signals.has("small-task") || signals.has("single-file")) {
    return result("stay-light", "high", score, signals);
  }

  return result("stay-light", "low", score, signals);
}

function normalize(input: string): string {
  return input.toLowerCase().replace(/\s+/g, " ").trim();
}

function hasAny(text: string, phrases: readonly string[]): boolean {
  return phrases.some((phrase) => text.includes(phrase));
}

function addSignalIf(
  signals: Set<LifecycleIntentSignal>,
  signal: LifecycleIntentSignal,
  condition: boolean,
): void {
  if (condition) signals.add(signal);
}

function hasMutationIntent(text: string): boolean {
  if (hasAny(text, NEGATED_MUTATION_PHRASES)) return false;
  return hasAny(text, MUTATION_PHRASES);
}

function hasSingleFileSignal(text: string): boolean {
  return (
    hasAny(text, SINGLE_FILE_PHRASES) ||
    text.includes(".md") ||
    text.includes(".ts") ||
    text.includes(".tsx") ||
    text.includes(".json")
  );
}

function isCancelOnly(signals: Set<LifecycleIntentSignal>): boolean {
  return signals.size === 1 && signals.has("cancel");
}

function scoreSignals(signals: Set<LifecycleIntentSignal>): number {
  let score = 0;
  if (signals.has("explicit-strict")) score += 5;
  if (signals.has("multi-file")) score += 2;
  if (signals.has("refactor")) score += 2;
  if (signals.has("migration")) score += 2;
  if (signals.has("dependency-change")) score += 3;
  if (signals.has("package-or-config")) score += 2;
  if (signals.has("failed-verification")) score += 2;
  if (signals.has("plan-revision")) score += 2;
  return score;
}

function result(
  recommendation: LifecycleIntentRecommendation,
  confidence: LifecycleIntentConfidence,
  score: number,
  signals: Set<LifecycleIntentSignal>,
): LifecycleIntentEvaluation {
  return {
    recommendation,
    confidence,
    score,
    signals: [...signals],
    modelVisible: false,
    enforced: false,
  };
}
