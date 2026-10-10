import { useMemo } from "react";
import { t } from "../i18n";
import type { ModelEntry, RoleAssignments, RoleOverride } from "../port/port";
import { activeKind, contextLabel, groupVendors, type Vendor } from "./Models";
import { orderAccounts, useProviderOrder } from "../state/providerorder";

type RoleKey = Exclude<keyof RoleAssignments, "web_search_effective" | "web_search_reason" | "web_search_source">;
type Answers = "chat" | "decision" | "search";

// Decision is the one job that cannot follow the main model: it asks a question
// set, which a chat model has no answer for, so its row offers the decision
// sources and says so when there are none.
const ROLES: [RoleKey, string, string, Answers][] = [
  ["planner", "计划", "仅生成计划，不写入", "chat"],
  ["subagent", "子代理", "派发的子任务", "chat"],
  ["vision", "看图", "处理主模型无法识别的图片", "chat"],
  ["guardian", "复核", "独立复核本轮", "chat"],
  ["decision", "决策", "system_one 询问的后端", "decision"],
];

const answersOf = (m: ModelEntry): Answers => (m.answers === "decision" ? "decision" : "chat");
const answersMatch = (m: ModelEntry, answers: Answers) => answers === "search" ? m.webSearch === true : answersOf(m) === answers;

// Only what the config or the catalog declares: an inferred "reads images" sends
// the user to a request the endpoint rejects.
function traits(m?: ModelEntry): string {
  if (!m) return "";
  const ctx = contextLabel(m.contextWindow);
  return [m.vision ? t("读图") : "", m.efforts && m.efforts.length > 1 ? t("可调推理") : "", ctx ? t("上下文 {size}", { size: ctx }) : ""]
    .filter(Boolean)
    .join(" · ");
}

interface Props {
  models: ModelEntry[];
  roles: RoleAssignments | null;
  main?: string;
  busy: string;
  // Which protocol each account is showing, as chosen on the services page.
  protocol: Record<string, string>;
  onMain: (ref: string) => void;
  onRole: (role: string, ref: string) => void;
  // Entries that win over a role's own row, so the row is not what runs.
  overrides?: Record<string, RoleOverride[]>;
  onClearOverride?: (role: string, key: string) => void;
}

// One row per job: what it is for, which model does it, and which service that
// model is reached through.
export function ModelUsage({ models, roles, main, busy, protocol, onMain, onRole, overrides, onClearOverride }: Props) {
  const order = useProviderOrder();
  const vendors = useMemo(() => orderAccounts(groupVendors(models), order), [models, order]);
  const serviceOf = (ref?: string) => vendors.find((v) => Object.values(v.byKind).some((list) => list.some((m) => m.ref === ref)))?.label ?? "";
  const byRef = (ref?: string) => models.find((m) => m.ref === ref);

  if (models.length === 0) return <div className="empty">{t("无法读取模型列表。")}</div>;

  // This pane is about the configuration, so its default row shows the model the
  // kernel starts new sessions on — the entry the catalog marks default — and not
  // whichever model the session in front happens to be on. `main` stays that
  // session model, which still decides each vendor's protocol.
  const configured = models.find((m) => m.default)?.ref;
  const shown = configured || main;
  const shownModel = byRef(shown);
  // What an attachment reaches: the vision role if assigned, else the sub-agent
  // it would be handed to, else the main model.
  const visionRef = roles?.vision || roles?.subagent || main;
  const visionModel = byRef(visionRef);

  return (
    <>
      <div className="usage" role="table" aria-label={t("模型用途")}>
        <div className="usage-row usage-hd" role="row">
          <span role="columnheader">{t("用途")}</span>
          <span role="columnheader">{t("使用模型")}</span>
          <span role="columnheader">{t("连接")}</span>
        </div>
        <div className="usage-row" role="row">
          <span className="usage-job" role="rowheader"><b>{t("默认模型")}</b><small>{t("当前对话和大多数任务")}</small></span>
          <span role="cell">
            <Choices vendors={vendors} protocol={protocol} main={main} value={shown ?? ""} answers="chat"
              label={t("默认模型")} disabled={busy !== ""} onPick={onMain} />
          </span>
          <span className="usage-conn" role="cell">{serviceOf(shown)}<small>{traits(shownModel)}</small></span>
        </div>
        {roles && ROLES.map(([key, name, tag, answers]) => {
          const set = roles[key];
          const offered = models.filter((m) => answersOf(m) === answers);
          const none = answers === "decision" && offered.length === 0;
          return (
            <div className="usage-row" role="row" key={key}>
              <span className="usage-job" role="rowheader"><b>{t(name)}</b><small>{t(tag)}</small></span>
              <span role="cell">
                <Choices vendors={vendors} protocol={protocol} main={main} value={set} answers={answers}
                  label={t(name)} role disabled={busy !== "" || none}
                  empty={t(answers === "chat" ? "跟随主模型" : none ? "尚无可用来源" : "不使用")}
                  onPick={(ref) => onRole(key, ref)} />
                {(overrides?.[key] ?? []).map((o) => (
                  <span className="usage-override" key={o.key} data-scope={o.scope}>
                    <span>{t("「{key}」已被配置固定为 {model}，这里的选择对它不起作用。", { key: o.key, model: o.model })}</span>
                    {o.scope === "user" ? (
                      <button type="button" data-action="roles.override.clear" disabled={busy !== ""} onClick={() => onClearOverride?.(key, o.key)}>
                        {t("改回跟随这里")}
                      </button>
                    ) : (
                      <small>{t("来自项目配置，需在项目里修改")}</small>
                    )}
                  </span>
                ))}
              </span>
              <span className="usage-conn" role="cell" data-follow={!set && answers === "chat" ? "" : undefined}>
                {set ? serviceOf(set) : answers === "chat" ? t("随主模型") : none ? t("在「模型服务」添加决策来源") : ""}
              </span>
            </div>
          );
        })}
        {roles && <SearchUsage models={models} vendors={vendors} protocol={protocol} main={main} roles={roles} busy={busy} onRole={onRole} />}
      </div>
      {!roles && <div className="empty">{t("无法读取角色分工。")}</div>}
      <p className="note">
        {visionModel?.vision
          ? t("主模型看不了的图会交给 {model}，它读图，所以附件真的会被看到。", { model: visionModel.model })
          : t("主模型无法识别的图片当前无人处理 —— 会在发送前被丢弃。为「看图」指定一个带「读图」标签的模型即可接管。")}
      </p>
    </>
  );
}

function searchReason(reason?: string) {
  switch (reason) {
    case "bad_ref": return t("搜索模型的格式应为 服务/模型");
    case "not_added": return t("搜索模型所在的连接已被移出");
    case "model_removed": return t("搜索模型已不在配置里");
    case "unsupported": return t("搜索模型的协议不支持原生搜索，或已关闭搜索");
    case "no_credentials": return t("搜索模型所在的连接没有凭据");
    default: return "";
  }
}

function SearchUsage({ models, vendors, protocol, main, roles, busy, onRole }: {
  models: ModelEntry[]; vendors: Vendor[]; protocol: Record<string, string>; main?: string;
  roles: RoleAssignments; busy: string; onRole: (role: string, ref: string) => void;
}) {
  const value = roles.web_search, effective = roles.web_search_effective ?? "";
  const ref = !value || value.toLowerCase() === "auto" ? "" : value;
  const none = !models.some((m) => m.webSearch);
  const serviceOf = (r: string) => vendors.find((v) => Object.values(v.byKind).flat().some((m) => m.ref === r))?.label ?? r;
  const reason = searchReason(roles.web_search_reason);
  const used = ref ? (reason || serviceOf(ref)) : effective
    ? t("自动：使用对话模型自带的搜索（{model}）", { model: serviceOf(effective) })
    : t("对话模型没有内置搜索；需要联网搜索时请选择一个搜索模型");
  return (
    <div className="usage-row" role="row">
      <span className="usage-job" role="rowheader"><b>{t("网页搜索")}</b><small>{t("独立搜索请求使用的模型")}</small></span>
      <span role="cell">
        <Choices vendors={vendors} protocol={protocol} main={main} value={ref} answers="search"
          label={t("网页搜索")} role disabled={busy !== ""} empty={t("自动选择")}
          onPick={(next) => onRole("web_search", next)} />
      </span>
      <span className="usage-conn" role="cell">
        {used}
        {none && <small>{t("尚无可用搜索模型")}</small>}
        {roles.web_search_source === "project" && <small>{effective
          ? t("项目配置覆盖：实际使用 {model}；此处保存的是全局设置。", { model: serviceOf(effective) })
          : t("项目配置覆盖了这一项；此处保存的是全局设置。")}</small>}
      </span>
    </div>
  );
}

function Choices({
  vendors, protocol, main, value, answers, label, role = false, disabled, empty, onPick,
}: {
  vendors: Vendor[]; protocol: Record<string, string>; main?: string; value: string; answers: Answers;
  label: string; role?: boolean; disabled: boolean; empty?: string; onPick: (ref: string) => void;
}) {
  // Each service offers the models of the protocol it is on, and a model already
  // chosen stays listed even when its service is showing the other protocol.
  const groups = vendors
    .map((v) => {
      const kind = protocol[v.key] ?? activeKind(v, main);
      const shown = (answers === "search" ? Object.values(v.byKind).flat() : v.byKind[kind] ?? []).filter((m) => answersMatch(m, answers));
      const held = Object.values(v.byKind).flat().find((m) => m.ref === value && answersMatch(m, answers) && !shown.includes(m));
      return { v, rows: held ? [held, ...shown] : shown };
    })
    .filter((g) => g.rows.length > 0);
  return (
    <select className="usage-pick" aria-label={label} data-action={role ? "roles.model" : "model.select"} value={value} disabled={disabled}
      onChange={(e) => onPick(e.target.value)}>
      {empty !== undefined && <option value="">{empty}</option>}
      {answers === "search" && value && !groups.some((g) => g.rows.some((m) => m.ref === value)) && <option value={value}>{t("{model}（不可用）", { model: value })}</option>}
      {groups.map(({ v, rows }) => (
        <optgroup key={v.key} label={v.label}>
          {rows.map((m) => (
            <option key={m.ref} value={m.ref}>{m.vision ? `${m.model} · ${t("读图")}` : m.model}</option>
          ))}
        </optgroup>
      ))}
    </select>
  );
}
