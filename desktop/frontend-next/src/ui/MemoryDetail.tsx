import { t } from "../i18n";
import type { MemoryEdit, MemoryEntry } from "../port/port";
import { Icon, StateIcon } from "./memicons";

export const TYPES: [string, string][] = [
  ["user", "用户"], ["feedback", "反馈"], ["project", "项目"], ["reference", "参考"], ["constraint", "约束"],
];
const TYPE_NAME = Object.fromEntries(TYPES);
const IMPORTANCE = ["", "低", "中", "高"];
const SCOPE: Record<string, string> = { project: "当前项目", global: "所有项目" };

export const typeOf = (m: MemoryEntry) => (m.type && TYPE_NAME[m.type] ? m.type : "project");

export function stateSaid(m: MemoryEntry): string {
  if (m.noDecay) return t("清晰，不会变淡");
  if (m.state === "faded") return t("已淡忘，自动想起时排在后面，仍能搜到");
  if (m.state === "fuzzy") return t("有些模糊，再被用到就会变清晰");
  return m.clearForDays ? t("清晰，约 {n} 天后才会变模糊", { n: m.clearForDays }) : t("清晰");
}

export function daysAgo(date: string | undefined, now = new Date()): string {
  if (!date) return t("还没被想起过");
  const days = Math.max(0, Math.round((now.getTime() - new Date(date + "T00:00:00").getTime()) / 86400000));
  return days === 0 ? t("今天被想起") : t("{n} 天前被想起", { n: days });
}

export function MemoryDetail({ m, edit, setEdit, busy, galaxies, onSave, onConfirm, onForget, onHistory, historyOpen, children }: {
  m: MemoryEntry;
  edit: MemoryEdit | null;
  setEdit: (e: MemoryEdit | null) => void;
  busy: boolean;
  galaxies: string[];
  onSave: () => void;
  onConfirm: () => void;
  onForget: () => void;
  onHistory: () => void;
  historyOpen: boolean;
  children?: React.ReactNode;
}) {
  if (edit?.name === m.name) {
    return (
      <div className="memdetail" data-target={m.name}>
        <div className="memedit">
          <label>{t("标题")}<input value={edit.title} onChange={(e) => setEdit({ ...edit, title: e.target.value })} /></label>
          <label>{t("简要说明")}<input value={edit.description} onChange={(e) => setEdit({ ...edit, description: e.target.value })} /></label>
          <label>{t("正文")}<textarea rows={8} value={edit.body} onChange={(e) => setEdit({ ...edit, body: e.target.value })} /></label>
          <div className="pair">
            <label>{t("类型")}
              <select value={edit.type} onChange={(e) => setEdit({ ...edit, type: e.target.value })}>
                {TYPES.map(([id, name]) => <option key={id} value={id}>{t(name)}</option>)}
              </select>
            </label>
            <label>{t("重要度")}
              <select value={String(edit.importance ?? 2)} onChange={(e) => setEdit({ ...edit, importance: Number(e.target.value) })}>
                {[3, 2, 1].map((n) => <option key={n} value={n}>{t(IMPORTANCE[n])}</option>)}
              </select>
            </label>
          </div>
          <label>{t("星系")}
            <input list="memgalaxies" value={edit.galaxy ?? ""} onChange={(e) => setEdit({ ...edit, galaxy: e.target.value })} />
            <datalist id="memgalaxies">{galaxies.map((g) => <option key={g} value={g} />)}</datalist>
          </label>
          <label>{t("生效时机")}
            <select value={edit.activation} onChange={(e) => setEdit({ ...edit, activation: e.target.value })}>
              <option value="relevant">{t("相关时启用")}</option>
              <option value="pinned">{t("每轮常驻")}</option>
            </select>
          </label>
          <div className="row">
            <button className="act" data-action="memory.save" data-target={m.name} disabled={busy} onClick={onSave}>
              {t(busy ? "正在保存…" : "保存")}
            </button>
            <button className="act ghost" data-action="memory.cancel" data-target={m.name} onClick={() => setEdit(null)}>{t("取消")}</button>
            <button className="act ghost" data-action="memory.forget" data-target={m.name} disabled={busy} onClick={onForget}>
              {t(busy ? "…" : "归档")}
            </button>
            <span className="hint">{t("保存将记为新版本，旧版本仍保留")}</span>
          </div>
        </div>
      </div>
    );
  }
  const facts: [string, string, React.ReactNode][] = [
    [typeOf(m), "类型", t(TYPE_NAME[typeOf(m)])],
    ["galaxy", "星系", m.galaxy || t("未归类")],
    ["importance", "重要度", t(IMPORTANCE[m.importance ?? 2] || "中")],
    ["", "状态", stateSaid(m)],
    ["clock", "最近", daysAgo(m.lastUsedAt)],
    ["uses", "用过", t("{n} 次", { n: m.uses ?? 0 })],
    ["pin", "生效", t(m.activation === "pinned" ? "每轮常驻" : "相关时才会检索")],
    ["project", "范围", t(SCOPE[m.scope ?? ""] ?? m.scope ?? "")],
  ];
  return (
    <div className="memdetail" data-target={m.name}>
      <div className="ti"><Icon name={typeOf(m)} size={18} /><span>{m.title || m.name}</span></div>
      {m.description && <p className="ds">{m.description}</p>}
      {m.usedLastTurn && m.why && <p className="why-used">{t("上一轮因「{why}」被检索到", { why: m.why })}</p>}
      {m.expired && <p className="stale">{t("已过期")}</p>}
      <dl className="facts">
        {facts.map(([icon, label, value]) => (
          <div key={label}>
            <dt>{icon ? <Icon name={icon} size={15} /> : <StateIcon state={m.noDecay ? "clear" : m.state} size={15} />}{t(label)}</dt>
            <dd>{value}</dd>
          </div>
        ))}
      </dl>
      <pre>{m.body?.trim() || t("（无正文）")}</pre>
      <div className="row">
        <button className="act" data-primary="" data-action="memory.confirm" data-target={m.name} disabled={busy} onClick={onConfirm}>
          <Icon name="check" size={14} />{t("没错")}
        </button>
        <button className="act ghost" data-action="memory.edit" data-target={m.name} onClick={() => setEdit({
          name: m.name, title: m.title ?? "", description: m.description ?? "", body: m.body ?? "",
          activation: m.activation, type: typeOf(m), galaxy: m.galaxy ?? "", importance: m.importance ?? 2,
        })}>
          <Icon name="edit" size={14} />{t("编辑")}
        </button>
        <button className="act ghost" data-action="memory.forget" data-target={m.name} disabled={busy} onClick={onForget}>
          <Icon name="archive" size={14} />{t(busy ? "…" : "归档")}
        </button>
      </div>
      <div className="row sub">
        {(m.revision ?? 1) > 1 && (
          <button className="lnk" data-action="memory.history" data-target={m.name} onClick={onHistory}>
            {t(historyOpen ? "收起旧版本" : "第 {n} 版，查看历史", { n: m.revision ?? 1 })}
          </button>
        )}
        {m.path && <span className="path">{m.path}</span>}
      </div>
      {children}
    </div>
  );
}

export function History({ list, current, busy, onRestore }: {
  list: MemoryEntry[] | undefined;
  current: number;
  busy: boolean;
  onRestore: (revision: number) => void;
}) {
  if (!list) return <div className="memhist"><span className="ds">{t("正在读取历史版本…")}</span></div>;
  const older = list.filter((m) => (m.revision ?? 1) !== current);
  if (older.length === 0) return <div className="memhist"><span className="ds">{t("仅有当前版本。")}</span></div>;
  return (
    <div className="memhist">
      {older.map((m) => (
        <div className="histrow" key={m.revision}>
          <span className="rev">{t("第 {n} 版", { n: m.revision ?? 1 })}</span>
          <span className="at">{m.updatedAt || m.createdAt}</span>
          <span className="ds" title={m.title}>{m.title}</span>
          <button className="act ghost" data-action="memory.restore" data-target={m.name} disabled={busy} onClick={() => onRestore(m.revision ?? 1)}>
            {t(busy ? "…" : "恢复此版本")}
          </button>
          <pre>{clipBody(m.body)}</pre>
        </div>
      ))}
      <span className="hint">{t("恢复同样记为新版本，历史版本均保留")}</span>
    </div>
  );
}

function clipBody(s: string | undefined): string {
  const body = (s ?? "").trim();
  if (!body) return "（无正文）";
  return body.length > 200 ? body.slice(0, 200) + "…" : body;
}
