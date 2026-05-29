export interface LifecyclePlanSuggestionRequest {
  text: string;
  codeMode: boolean;
  planMode: boolean;
  lifecycleMode: "off" | "strict";
  alreadySuggested?: boolean;
}

export function shouldSuggestPlanForEngineeringIntent(
  request: LifecyclePlanSuggestionRequest,
): boolean {
  return (
    request.codeMode &&
    !request.planMode &&
    request.lifecycleMode === "off" &&
    !request.alreadySuggested &&
    detectLifecyclePlanSuggestion(request.text)
  );
}

export function detectLifecyclePlanSuggestion(text: string): boolean {
  const normalized = normalizePrompt(text);
  if (!normalized) return false;

  return (
    ENGLISH_HIGH_RISK_PATTERNS.some((pattern) => pattern.test(normalized)) ||
    CHINESE_HIGH_RISK_PATTERNS.some((pattern) => pattern.test(normalized))
  );
}

function normalizePrompt(text: string): string {
  return text.normalize("NFKC").toLowerCase().replace(/\s+/g, " ").trim();
}

const EN_REFACTOR_ACTION = "(?:refactor|rewrite|restructure|extract|split|move|rename|migrate)";
const EN_MULTI_FILE_SCOPE = "(?:multi-file|multiple files|several files|many files)";
const EN_CROSS_SCOPE = "(?:across\\s+(?:the\\s+)?(?:app|codebase|repo|files|modules|packages))";
const EN_REFERENCE_TARGET = "(?:imports?|exports?|references?|callers?|usages?)";
const EN_CONFIG_TARGET =
  "(?:dependencies|dependency|package\\.json|package-lock\\.json|pnpm-lock\\.yaml|yarn\\.lock|tsconfig(?:\\.[\\w-]+)?\\.json|lockfile|config(?:uration)?)";
const EN_CONFIG_ACTION = "(?:migrate|migration|upgrade|install|update|bump|switch|replace)";

const ENGLISH_HIGH_RISK_PATTERNS = [
  new RegExp(`\\b${EN_MULTI_FILE_SCOPE}\\b.{0,80}\\b${EN_REFACTOR_ACTION}\\b`),
  new RegExp(
    `\\b${EN_REFACTOR_ACTION}\\b.{0,80}\\b(?:${EN_MULTI_FILE_SCOPE}|${EN_CROSS_SCOPE})\\b`,
  ),
  new RegExp(`\\brename\\b.{0,60}\\bapi\\b.{0,100}\\bupdate\\b.{0,60}\\b${EN_REFERENCE_TARGET}\\b`),
  new RegExp(
    `\\bmove\\b.{0,80}\\b(?:module|package|directory|folder|src\\/[^\\s]+)\\b.{0,100}\\bupdate\\b.{0,60}\\b${EN_REFERENCE_TARGET}\\b`,
  ),
  new RegExp(`\\b${EN_CONFIG_ACTION}\\b.{0,100}\\b${EN_CONFIG_TARGET}\\b`),
  new RegExp(`\\b${EN_CONFIG_TARGET}\\b.{0,100}\\b${EN_CONFIG_ACTION}\\b`),
];

const CHINESE_HIGH_RISK_PATTERNS = [
  /(?:重构|抽取|拆分|迁移|移动|改名).{0,24}(?:多个文件|多文件|多个模块|整个项目|整个仓库|全项目)/,
  /(?:多个文件|多文件|多个模块|整个项目|整个仓库|全项目).{0,24}(?:重构|抽取|拆分|迁移|移动|改名)/,
  /(?:移动|迁移|改名|重命名).{0,24}(?:模块|目录|包|api).{0,40}(?:更新|修改|替换).{0,24}(?:引用|导入|调用)/,
  /(?:迁移|升级|更新|替换).{0,24}(?:依赖|配置|lockfile|package\.json|pnpm-lock\.yaml|tsconfig)/,
];
