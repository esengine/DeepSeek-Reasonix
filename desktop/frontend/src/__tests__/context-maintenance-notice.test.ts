import { readFileSync } from "node:fs";
import {
  formatContextMaintenanceNotice,
  isNewMaintenanceOperation,
  rememberMaintenanceOperation,
} from "../lib/contextMaintenanceTypes";
import type { DictKey, Translator } from "../lib/i18n";

function ok(value: unknown, message: string) {
  if (!value) throw new Error(message);
}

const messages: Partial<Record<DictKey, string>> = {
  "context.maintenanceTitle": "上下文短视图",
  "context.maintenanceAppliedSummary": "已生成短视图",
  "context.maintenanceBlockedSummary": "摘要未形成短视图 · 已停重试",
  "context.maintenanceFailedSummary": "摘要失败 · 已停重试",
  "context.tokensValue": "{value} tokens",
  "summary.detail": "摘要",
};

const translate: Translator = (key, vars) => {
  const value = messages[key] ?? key;
  return value.replace(/\{(\w+)\}/g, (_, name: string) => String(vars?.[name] ?? `{${name}}`));
};

const applied = formatContextMaintenanceNotice({
  status: "applied",
  action: "summary",
  inputTokens: 120,
  resultTokens: 80,
  savedTokens: 40,
}, translate);
ok(applied === "已生成短视图", `unexpected applied notice: ${applied}`);

const blocked = formatContextMaintenanceNotice({ status: "blocked" }, translate);
ok(blocked === "摘要未形成短视图 · 已停重试", `unexpected blocked notice: ${blocked}`);

const failed = formatContextMaintenanceNotice({ status: "failed" }, translate);
ok(failed === "摘要失败 · 已停重试", `unexpected failed notice: ${failed}`);

const contextPanelSource = readFileSync(new URL("../components/ContextPanel.tsx", import.meta.url), "utf8");
ok(
  !contextPanelSource.includes('className="context-panel__maintenance"'),
  "ContextPanel must not render the context checkpoint detail block",
);
ok(
  !contextPanelSource.includes('t("context.maintenanceCanonical")')
    && !contextPanelSource.includes('t("context.maintenanceCheckpoint")'),
  "ContextPanel must not expose canonical, model-visible, or checkpoint details",
);
ok(
  !contextPanelSource.includes("checkpointState")
    && !contextPanelSource.includes("canonicalTokens")
    && !contextPanelSource.includes("projectedTokens"),
  "ContextPanel must not derive hidden checkpoint presentation state",
);
ok(
  !contextPanelSource.includes("snipTrigger") && !contextPanelSource.includes("forceTrigger"),
  "ContextPanel must not present retired multi-threshold triggers as user settings",
);

ok(isNewMaintenanceOperation([], "op-1"), "empty seen list accepts first operationId");
ok(isNewMaintenanceOperation(["op-1"], "op-1") === false, "duplicate operationId is rejected");
ok(isNewMaintenanceOperation(["op-1"], "op-2"), "distinct operationId is accepted");
ok(isNewMaintenanceOperation(["op-1"], ""), "missing operationId is still shown");
const remembered = rememberMaintenanceOperation(["op-1"], "op-2");
ok(remembered.includes("op-1") && remembered.includes("op-2"), "remember keeps prior and new ids");
ok(rememberMaintenanceOperation(["op-1"], "op-1").length === 1, "remember is idempotent for same id");
const many = Array.from({ length: 70 }, (_, i) => `op-${i}`);
const bounded = rememberMaintenanceOperation(many.slice(0, 64), "op-new");
ok(bounded.length === 64 && bounded[bounded.length - 1] === "op-new", "remember bounds to 64 ids");

const settingsSource = readFileSync(new URL("../components/SettingsPanel.tsx", import.meta.url), "utf8");
ok(
  !settingsSource.includes("settings.coldResumePrune") && !settingsSource.includes("SetColdResumePrune"),
  "SettingsPanel must not expose retired coldResumePrune control",
);

console.log("context-maintenance-notice: ok");
