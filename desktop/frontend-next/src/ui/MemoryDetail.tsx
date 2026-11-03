import { t } from "../i18n";
import type { MemoryEdit, MemoryEntry } from "../port/port";
import { Icon, StateIcon } from "./memicons";

export const TYPES: [string, string][] = [
  ["user", "common.user"], ["feedback", "common.feedback"], ["project", "common.project"], ["reference", "memoryDetail.reference"], ["constraint", "common.constraints"],
];
const TYPE_NAME = Object.fromEntries(TYPES);
const IMPORTANCE = ["", "memoryDetail.low", "memoryDetail.medium", "memoryDetail.high"];
const SCOPE: Record<string, string> = { project: "common.thisProject", global: "memoryDetail.allProjects" };

export const typeOf = (m: MemoryEntry) => (m.type && TYPE_NAME[m.type] ? m.type : "project");

export function stateSaid(m: MemoryEntry): string {
  if (m.noDecay) return t("memoryDetail.clearAndItNeverFades");
  if (m.state === "faded") return t("memoryDetail.fadedRecalledAfterEverythingElse");
  if (m.state === "fuzzy") return t("memoryDetail.aLittleFuzzyUsingIt");
  return m.clearForDays ? t("memoryDetail.clearForAboutMoreDays", { n: m.clearForDays }) : t("memoryDetail.clear");
}

export function daysAgo(date: string | undefined, now = new Date()): string {
  if (!date) return t("memoryDetail.notRecalledYet");
  const days = Math.max(0, Math.round((now.getTime() - new Date(date + "T00:00:00").getTime()) / 86400000));
  return days === 0 ? t("memoryDetail.recalledToday") : t("memoryDetail.recalledDaysAgo", { n: days });
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
          <label>{t("common.titling")}<input value={edit.title} onChange={(e) => setEdit({ ...edit, title: e.target.value })} /></label>
          <label>{t("memoryDetail.oneLineSummary")}<input value={edit.description} onChange={(e) => setEdit({ ...edit, description: e.target.value })} /></label>
          <label>{t("common.bodyText")}<textarea rows={8} value={edit.body} onChange={(e) => setEdit({ ...edit, body: e.target.value })} /></label>
          <div className="pair">
            <label>{t("common.type")}
              <select value={edit.type} onChange={(e) => setEdit({ ...edit, type: e.target.value })}>
                {TYPES.map(([id, name]) => <option key={id} value={id}>{t(name)}</option>)}
              </select>
            </label>
            <label>{t("memoryDetail.importance")}
              <select value={String(edit.importance ?? 2)} onChange={(e) => setEdit({ ...edit, importance: Number(e.target.value) })}>
                {[3, 2, 1].map((n) => <option key={n} value={n}>{t(IMPORTANCE[n])}</option>)}
              </select>
            </label>
          </div>
          <label>{t("common.galaxy")}
            <input list="memgalaxies" value={edit.galaxy ?? ""} onChange={(e) => setEdit({ ...edit, galaxy: e.target.value })} />
            <datalist id="memgalaxies">{galaxies.map((g) => <option key={g} value={g} />)}</datalist>
          </label>
          <label>{t("memoryDetail.whenItApplies")}
            <select value={edit.activation} onChange={(e) => setEdit({ ...edit, activation: e.target.value })}>
              <option value="relevant">{t("memoryDetail.recalledWhenRelevant2")}</option>
              <option value="pinned">{t("memoryDetail.inEveryTurn")}</option>
            </select>
          </label>
          <div className="row">
            <button className="act" data-action="memory.save" data-target={m.name} disabled={busy} onClick={onSave}>
              {t(busy ? "common.saving" : "common.save")}
            </button>
            <button className="act ghost" data-action="memory.cancel" data-target={m.name} onClick={() => setEdit(null)}>{t("common.cancel")}</button>
            <button className="act ghost" data-action="memory.forget" data-target={m.name} disabled={busy} onClick={onForget}>
              {t(busy ? "…" : "common.archived")}
            </button>
            <span className="hint">{t("memoryDetail.savingRecordsANewRevision")}</span>
          </div>
        </div>
      </div>
    );
  }
  const facts: [string, string, React.ReactNode][] = [
    [typeOf(m), "common.type", t(TYPE_NAME[typeOf(m)])],
    ["galaxy", "common.galaxy", m.galaxy || t("common.noGalaxy")],
    ["importance", "memoryDetail.importance", t(IMPORTANCE[m.importance ?? 2] || "memoryDetail.medium")],
    ["", "memoryDetail.state", stateSaid(m)],
    ["clock", "common.recent", daysAgo(m.lastUsedAt)],
    ["uses", "memoryDetail.used", t("memoryDetail.times", { n: m.uses ?? 0 })],
    ["pin", "memoryDetail.applies", t(m.activation === "pinned" ? "memoryDetail.inEveryTurn" : "memoryDetail.recalledWhenRelevant")],
    ["project", "memoryDetail.scope", t(SCOPE[m.scope ?? ""] ?? m.scope ?? "")],
  ];
  return (
    <div className="memdetail" data-target={m.name}>
      <div className="ti"><Icon name={typeOf(m)} size={18} /><span>{m.title || m.name}</span></div>
      {m.description && <p className="ds">{m.description}</p>}
      {m.usedLastTurn && m.why && <p className="why-used">{t("memoryDetail.surfacedLastTurnBecauseOf", { why: m.why })}</p>}
      {m.expired && <p className="stale">{t("memoryDetail.expired")}</p>}
      <dl className="facts">
        {facts.map(([icon, label, value]) => (
          <div key={label}>
            <dt>{icon ? <Icon name={icon} size={15} /> : <StateIcon state={m.noDecay ? "clear" : m.state} size={15} />}{t(label)}</dt>
            <dd>{value}</dd>
          </div>
        ))}
      </dl>
      <pre>{m.body?.trim() || t("memoryDetail.noBody")}</pre>
      <div className="row">
        <button className="act" data-primary="" data-action="memory.confirm" data-target={m.name} disabled={busy} onClick={onConfirm}>
          <Icon name="check" size={14} />{t("memoryDetail.thatSRight")}
        </button>
        <button className="act ghost" data-action="memory.edit" data-target={m.name} onClick={() => setEdit({
          name: m.name, title: m.title ?? "", description: m.description ?? "", body: m.body ?? "",
          activation: m.activation, type: typeOf(m), galaxy: m.galaxy ?? "", importance: m.importance ?? 2,
        })}>
          <Icon name="edit" size={14} />{t("common.edit")}
        </button>
        <button className="act ghost" data-action="memory.forget" data-target={m.name} disabled={busy} onClick={onForget}>
          <Icon name="archive" size={14} />{t(busy ? "…" : "common.archived")}
        </button>
      </div>
      <div className="row sub">
        {(m.revision ?? 1) > 1 && (
          <button className="lnk" data-action="memory.history" data-target={m.name} onClick={onHistory}>
            {t(historyOpen ? "memoryDetail.hideOlderRevisions" : "memoryDetail.revisionSeeOlder", { n: m.revision ?? 1 })}
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
  if (!list) return <div className="memhist"><span className="ds">{t("memoryDetail.readingOlderRevisions")}</span></div>;
  const older = list.filter((m) => (m.revision ?? 1) !== current);
  if (older.length === 0) return <div className="memhist"><span className="ds">{t("memoryDetail.onlyTheCurrentRevisionExists")}</span></div>;
  return (
    <div className="memhist">
      {older.map((m) => (
        <div className="histrow" key={m.revision}>
          <span className="rev">{t("memoryDetail.revision", { n: m.revision ?? 1 })}</span>
          <span className="at">{m.updatedAt || m.createdAt}</span>
          <span className="ds" title={m.title}>{m.title}</span>
          <button className="act ghost" data-action="memory.restore" data-target={m.name} disabled={busy} onClick={() => onRestore(m.revision ?? 1)}>
            {t(busy ? "…" : "memoryDetail.restoreThisOne")}
          </button>
          <pre>{clipBody(m.body)}</pre>
        </div>
      ))}
      <span className="hint">{t("memoryDetail.restoringRecordsANewRevision")}</span>
    </div>
  );
}

function clipBody(s: string | undefined): string {
  const body = (s ?? "").trim();
  if (!body) return "memoryDetail.noBody";
  return body.length > 200 ? body.slice(0, 200) + "…" : body;
}
