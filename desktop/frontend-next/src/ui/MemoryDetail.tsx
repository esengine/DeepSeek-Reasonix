import { t } from "../i18n";
import type { MemoryEdit, MemoryEntry } from "../port/port";
import { Icon } from "./memicons";
import { Button } from "./kit/Button";

export const TYPES: [string, string][] = [
  ["user", "remote.user"], ["feedback", "feedback.feedback"], ["project", "common.project"], ["reference", "memory.reference"], ["constraint", "memory.constraints"],
];
export const TYPE_NAME: Record<string, string> = Object.fromEntries(TYPES);
const IMPORTANCE = ["", "memory.low", "memory.medium", "memory.high"];
const SCOPE: Record<string, string> = { project: "session.this_project", global: "memory.all_projects" };

export const typeOf = (m: MemoryEntry) => (m.type && TYPE_NAME[m.type] ? m.type : "project");

export function stateSaid(m: MemoryEntry): string {
  if (m.noDecay) return t("memory.clear_and_it_never_fades");
  if (m.state === "faded") return t("memory.faded_recalled_after_everything_else");
  if (m.state === "fuzzy") return t("memory.a_little_fuzzy_using_it");
  return m.clearForDays ? t("memory.clear_for_about_n_more", { n: m.clearForDays }) : t("memory.clear");
}

export function daysAgo(date: string | undefined, now = new Date()): string {
  if (!date) return t("memory.not_recalled_yet");
  const days = Math.max(0, Math.round((now.getTime() - new Date(date + "T00:00:00").getTime()) / 86400000));
  return days === 0 ? t("memory.recalled_today") : t("memory.recalled_n_days_ago", { n: days });
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
          <label>{t("memory.titling")}<input value={edit.title} onChange={(e) => setEdit({ ...edit, title: e.target.value })} /></label>
          <label>{t("memory.one_line_summary")}<input value={edit.description} onChange={(e) => setEdit({ ...edit, description: e.target.value })} /></label>
          <label>{t("common.body_text")}<textarea rows={8} value={edit.body} onChange={(e) => setEdit({ ...edit, body: e.target.value })} /></label>
          <div className="pair">
            <label>{t("common.type")}
              <select value={edit.type} onChange={(e) => setEdit({ ...edit, type: e.target.value })}>
                {TYPES.map(([id, name]) => <option key={id} value={id}>{t(name)}</option>)}
              </select>
            </label>
            <label>{t("memory.importance")}
              <select value={String(edit.importance ?? 2)} onChange={(e) => setEdit({ ...edit, importance: Number(e.target.value) })}>
                {[3, 2, 1].map((n) => <option key={n} value={n}>{t(IMPORTANCE[n])}</option>)}
              </select>
            </label>
          </div>
          <label>{t("memory.galaxy")}
            <input list="memgalaxies" value={edit.galaxy ?? ""} onChange={(e) => setEdit({ ...edit, galaxy: e.target.value })} />
            <datalist id="memgalaxies">{galaxies.map((g) => <option key={g} value={g} />)}</datalist>
          </label>
          <label>{t("memory.when_it_applies")}
            <select value={edit.activation} onChange={(e) => setEdit({ ...edit, activation: e.target.value })}>
              <option value="relevant">{t("memory.recalled_when_relevant")}</option>
              <option value="pinned">{t("memory.in_every_turn")}</option>
            </select>
          </label>
          <div className="row">
            <Button data-action="memory.save" data-target={m.name} disabled={busy} onClick={onSave}>
              {t(busy ? "common.saving_2" : "common.save")}
            </Button>
            <Button variant="ghost" data-action="memory.cancel" data-target={m.name} onClick={() => setEdit(null)}>{t("common.cancel")}</Button>
            <Button variant="ghost" data-action="memory.forget" data-target={m.name} disabled={busy} onClick={onForget}>
              {t(busy ? "…" : "common.archived_2")}
            </Button>
            <span className="hint">{t("memory.saving_records_a_new_revision")}</span>
          </div>
        </div>
      </div>
    );
  }
  const content: [string, React.ReactNode][] = [
    ["common.type", t(TYPE_NAME[typeOf(m)])],
    ["memory.galaxy", m.galaxy || t("memory.no_galaxy")],
    ["memory.importance", t(IMPORTANCE[m.importance ?? 2] || "memory.medium")],
    ["memory.scope", t(SCOPE[m.scope ?? ""] ?? m.scope ?? "")],
  ];
  const use: [string, React.ReactNode][] = [
    ["memory.state", stateSaid(m)],
    ["session.recent", daysAgo(m.lastUsedAt)],
    ["memory.used", t("memory.n_times", { n: m.uses ?? 0 })],
    ["memory.applies", t(m.activation === "pinned" ? "memory.in_every_turn" : "memory.recalled_when_relevant_2")],
  ];
  const group = (title: string, rows: [string, React.ReactNode][]) => (
    <section aria-label={title}>
      <h4>{title}</h4>
      <dl>
        {rows.map(([label, value]) => (
          <div key={label}><dt>{t(label)}</dt><dd>{value}</dd></div>
        ))}
      </dl>
    </section>
  );
  return (
    <div className="memdetail" data-target={m.name}>
      <div className="memhead">
        <div className="memhead-tx">
          <div className="ti">{m.title || m.name}</div>
          {m.description && <p className="ds">{m.description}</p>}
        </div>
        <Button variant="primary" data-action="memory.confirm" data-target={m.name} disabled={busy} onClick={onConfirm}>
          <Icon name="check" size={13} />{t("memory.thats_right")}
        </Button>
        <Button data-action="memory.edit" data-target={m.name} onClick={() => setEdit({
          name: m.name, title: m.title ?? "", description: m.description ?? "", body: m.body ?? "",
          activation: m.activation, type: typeOf(m), galaxy: m.galaxy ?? "", importance: m.importance ?? 2,
        })}>
          <Icon name="edit" size={13} />{t("common.edit")}
        </Button>
        <Button variant="ghost" iconOnly data-action="memory.forget" data-target={m.name} disabled={busy} onClick={onForget}
          aria-label={t(busy ? "…" : "common.archived_2")} title={t("common.archived_2")}>
          <Icon name="archive" size={15} />
        </Button>
      </div>
      {m.usedLastTurn && m.why && <p className="why-used">{t("memory.surfaced_last_turn_because_of", { why: m.why })}</p>}
      {m.expired && <p className="stale">{t("memory.expired")}</p>}
      <pre>{m.body?.trim() || t("memory.no_body")}</pre>
      <div className="facts">
        {group(t("memory.content"), content)}
        {group(t("memory.use"), use)}
      </div>
      <div className="row sub">
        {(m.revision ?? 1) > 1 && (
          <button className="lnk" data-action="memory.history" data-target={m.name} onClick={onHistory}>
            {t(historyOpen ? "memory.hide_older_revisions" : "memory.revision_n_see_older", { n: m.revision ?? 1 })}
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
  if (!list) return <div className="memhist"><span className="ds">{t("memory.reading_older_revisions")}</span></div>;
  const older = list.filter((m) => (m.revision ?? 1) !== current);
  if (older.length === 0) return <div className="memhist"><span className="ds">{t("memory.only_the_current_revision_exists")}</span></div>;
  return (
    <div className="memhist">
      {older.map((m) => (
        <div className="histrow" key={m.revision}>
          <span className="rev">{t("memory.revision_n", { n: m.revision ?? 1 })}</span>
          <span className="at">{m.updatedAt || m.createdAt}</span>
          <span className="ds" title={m.title}>{m.title}</span>
          <Button variant="ghost" data-action="memory.restore" data-target={m.name} disabled={busy} onClick={() => onRestore(m.revision ?? 1)}>
            {t(busy ? "…" : "memory.restore_this_one")}
          </Button>
          <pre>{clipBody(m.body)}</pre>
        </div>
      ))}
      <span className="hint">{t("memory.restoring_records_a_new_revision")}</span>
    </div>
  );
}

function clipBody(s: string | undefined): string {
  const body = (s ?? "").trim();
  if (!body) return t("memory.no_body");
  return body.length > 200 ? body.slice(0, 200) + "…" : body;
}
