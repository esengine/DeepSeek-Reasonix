import { useMemo } from "react";
import { t } from "../i18n";
import type { ModelEntry, RoleAssignments } from "../port/port";
import { activeKind, contextLabel, groupVendors, type Vendor } from "./Models";
import { orderAccounts, useProviderOrder } from "../state/providerorder";

type RoleKey = keyof RoleAssignments;
type Answers = "chat" | "decision";

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
}

// One row per job: what it is for, which model does it, and which service that
// model is reached through.
export function ModelUsage({ models, roles, main, busy, protocol, onMain, onRole }: Props) {
  const order = useProviderOrder();
  const vendors = useMemo(() => orderAccounts(groupVendors(models), order), [models, order]);
  const serviceOf = (ref?: string) => vendors.find((v) => Object.values(v.byKind).some((list) => list.some((m) => m.ref === ref)))?.label ?? "";
  const byRef = (ref?: string) => models.find((m) => m.ref === ref);

  if (models.length === 0) return <div className="empty">{t("无法读取模型列表。")}</div>;

  const current = byRef(main);
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
            <Choices vendors={vendors} protocol={protocol} main={main} value={main ?? ""} answers="chat"
              label={t("默认模型")} disabled={busy !== ""} onPick={onMain} />
          </span>
          <span className="usage-conn" role="cell">{serviceOf(main)}<small>{traits(current)}</small></span>
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
              </span>
              <span className="usage-conn" role="cell" data-follow={!set && answers === "chat" ? "" : undefined}>
                {set ? serviceOf(set) : answers === "chat" ? t("随主模型") : none ? t("在「模型服务」添加决策来源") : ""}
              </span>
            </div>
          );
        })}
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
      const shown = (v.byKind[kind] ?? []).filter((m) => answersOf(m) === answers);
      const held = Object.values(v.byKind).flat().find((m) => m.ref === value && !shown.includes(m));
      return { v, rows: held ? [held, ...shown] : shown };
    })
    .filter((g) => g.rows.length > 0);
  return (
    <select className="usage-pick" aria-label={label} data-action={role ? "roles.model" : "model.select"} value={value} disabled={disabled}
      onChange={(e) => onPick(e.target.value)}>
      {empty !== undefined && <option value="">{empty}</option>}
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
