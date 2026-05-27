export interface ExplicitPlanIntentRequest {
  text: string;
  codeMode: boolean;
  planMode: boolean;
}

export function shouldEnterPlanModeForExplicitIntent(request: ExplicitPlanIntentRequest): boolean {
  return request.codeMode && !request.planMode && detectExplicitPlanFirstIntent(request.text);
}

export function detectExplicitPlanFirstIntent(text: string): boolean {
  const normalized = normalizePrompt(text);
  if (!normalized) return false;

  return (
    hasExplicitStrictLifecycleRequest(normalized) ||
    ENGLISH_PLAN_FIRST_PATTERNS.some((pattern) => pattern.test(normalized)) ||
    CHINESE_PLAN_FIRST_PATTERNS.some((pattern) => pattern.test(normalized))
  );
}

function normalizePrompt(text: string): string {
  return text.normalize("NFKC").toLowerCase().replace(/\s+/g, " ").trim();
}

function hasExplicitStrictLifecycleRequest(text: string): boolean {
  return EXPLICIT_STRICT_PATTERNS.some((pattern) => pattern.test(text));
}

const EN_ACTION =
  "(?:edit|editing|change|changing|modify|modifying|implement|implementing|code|coding|write|writing|touch|touching|refactor|refactoring|migrate|migrating|rename|renaming|move|moving|files?|codebase)";
const EN_PLAN = "(?:implementation plan|migration plan|refactor plan|plan|proposal|outline)";

const EXPLICIT_STRICT_PATTERNS = [
  /\B\/plan\s+strict\b/,
  /\b(?:use|enable|turn on|start|activate)\s+(?:the\s+)?(?:(?:strict\s+)?engineering\s+lifecycle|strict\s+lifecycle)\b/,
  /\b(?:use|enable|turn on|start|activate)\s+(?:the\s+)?plan mode\b/,
  /(?:使用|启用|开启|进入).{0,12}(?:严格|强约束|strict).{0,12}(?:lifecycle|生命周期|工程生命周期|plan|计划模式)/,
];

const ENGLISH_PLAN_FIRST_PATTERNS = [
  new RegExp(`\\bplan\\s+first\\b.{0,80}\\b${EN_ACTION}\\b`),
  new RegExp(
    `\\b(?:draft|write|prepare|create|make|show me|give me)\\s+(?:an?\\s+)?${EN_PLAN}\\b.{0,80}\\b(?:before|prior to)\\b.{0,80}\\b${EN_ACTION}\\b`,
  ),
  new RegExp(`\\b(?:before|prior to)\\b.{0,80}\\b${EN_ACTION}\\b.{0,80}\\b${EN_PLAN}\\b`),
  new RegExp(
    `\\b(?:do not|don't|dont|avoid)\\b.{0,50}\\b${EN_ACTION}\\b.{0,80}\\b(?:first\\s+)?${EN_PLAN}\\b`,
  ),
  new RegExp(`\\b${EN_PLAN}\\b.{0,50}\\b(?:before|then)\\b.{0,80}\\b${EN_ACTION}\\b`),
];

const CHINESE_PLAN_FIRST_PATTERNS = [
  /先.{0,16}(?:规划|计划|方案).{0,40}(?:再|然后|之后)?.{0,16}(?:改|修改|写|实现|编码|动代码|动手|重构|迁移)/,
  /先.{0,20}(?:给我|帮我|做|出|制定|设计|写)?.{0,8}(?:实现方案|计划|规划|方案).{0,40}(?:不要|别).{0,20}(?:直接)?(?:改|修改|写|实现|动代码|动手)/,
  /(?:先不要|先别|不要|别).{0,20}(?:改|修改|写|实现|动代码|动手).{0,40}(?:先|先做|先给|先出)?.{0,20}(?:计划|规划|方案)/,
];
