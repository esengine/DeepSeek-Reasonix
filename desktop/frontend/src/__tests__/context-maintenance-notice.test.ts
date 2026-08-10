import { readFileSync } from "node:fs";
import { formatContextMaintenanceNotice } from "../lib/contextMaintenanceTypes";
import type { DictKey, Translator } from "../lib/i18n";

function ok(value: unknown, message: string) {
  if (!value) throw new Error(message);
}

const messages: Partial<Record<DictKey, string>> = {
  "context.maintenanceTitle": "上下文维护",
  "context.maintenanceActionPrune": "工具结果裁剪",
  "context.tokensValue": "{value} tokens",
  "projectTree.status.paused": "已暂停",
  "settings.typography.applied": "已应用",
  "task.state.failed": "失败",
};

const translate: Translator = (key, vars) => {
  const value = messages[key] ?? key;
  return value.replace(/\{(\w+)\}/g, (_, name: string) => String(vars?.[name] ?? `{${name}}`));
};

const applied = formatContextMaintenanceNotice({
  status: "applied",
  action: "prune",
  inputTokens: 120,
  resultTokens: 80,
  savedTokens: 40,
}, translate);
ok(applied === "上下文维护 · 工具结果裁剪 · 已应用 · 120 → 80 tokens · −40 tokens", `unexpected applied notice: ${applied}`);
ok(!applied.includes("Context") && !applied.includes("applied"), "maintenance notice leaked hardcoded English");

const blocked = formatContextMaintenanceNotice({ status: "blocked" }, translate);
ok(blocked === "上下文维护 · 已暂停", `unexpected blocked notice: ${blocked}`);

const contextPanelSource = readFileSync(new URL("../components/ContextPanel.tsx", import.meta.url), "utf8");
ok(
  !contextPanelSource.includes('t("context.maintenanceProjected")'),
  "ContextPanel must not render the internal context-maintenance composition block",
);
ok(
  !contextPanelSource.includes("const maintenance = context?.maintenance"),
  "ContextPanel must not derive hidden maintenance UI state",
);

console.log("context-maintenance-notice: ok");
