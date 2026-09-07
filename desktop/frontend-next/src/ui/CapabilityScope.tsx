import { useState } from "react";
import { t } from "../i18n";
import type { CapabilityScope } from "../port/port";

// A switch flipped here answers for this project, not for every project. That
// is the useful default and also the invisible one, so the row that carries an
// exception says so and offers the way back — without it the same list would
// read identically in a project that simply inherited everything.
export function Exception({ onClear, busy }: { onClear: () => void; busy: boolean }) {
  return (
    <button className="exc" data-action="capability.scope" disabled={busy} title={t("恢复成跟随全局")} onClick={onClear}>
      {t("仅本项目")}
    </button>
  );
}

// Which folder this page is answering for. The shell holds several projects at
// once and the pane follows whichever one is in front, so the heading has to
// name it — otherwise switching tabs silently changes the list under an
// unchanged title. Worktrees of one repository share one answer, and saying how
// many is what stops "my settings vanished on a new branch". The name is also
// the way out: a project can be managed without the session moving to it.
export function ScopeBar({
  scope, scopes, onPick,
}: {
  scope: CapabilityScope; scopes: CapabilityScope[]; onPick: (root: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const shared = scope.repo && (scope.trees ?? 1) > 1;
  const elsewhere = !scope.current;
  return (
    <div className="scopebar" data-away={elsewhere ? "" : undefined}>
      <button className="who" aria-expanded={open} onClick={() => setOpen(!open)}>
        {scope.label || scope.name}
        <i className="cv" aria-hidden>▾</i>
      </button>
      <span className="pt" title={scope.root}>{scope.root}</span>
      {scope.branch && <span className="br">{scope.branch}</span>}
      {shared && <span className="sh">{t("{n} 个工作树共用这份设置", { n: scope.trees ?? 1 })}</span>}
      {!scope.repo && <span className="sh">{t("不是 git 仓库，按目录单独记")}</span>}
      <span className="ov">
        {scope.overrides ? t("{n} 项只在这里生效", { n: scope.overrides }) : t("没有本项目的例外")}
      </span>
      {elsewhere && <span className="away">{t("不是当前会话所在的项目 —— 只能改开关")}</span>}
      {open && (
        <ScopePicker
          scopes={scopes}
          at={scope.root}
          onPick={(root) => {
            setOpen(false);
            onPick(root);
          }}
        />
      )}
    </div>
  );
}

// With a folder or two the list is the whole answer. Past that, the projects
// worth finding are the ones already carrying an exception — so that is the
// filter, and search only appears when the list is long enough to need it.
function ScopePicker({
  scopes, at, onPick,
}: {
  scopes: CapabilityScope[]; at: string; onPick: (root: string) => void;
}) {
  const [q, setQ] = useState("");
  const [onlyChanged, setOnlyChanged] = useState(false);
  const searchable = scopes.length > 8;
  const shown = scopes.filter((sc) => {
    if (onlyChanged && !sc.overrides) return false;
    const needle = q.trim().toLowerCase();
    return !needle || (sc.label || sc.name).toLowerCase().includes(needle) || sc.root.toLowerCase().includes(needle);
  });
  return (
    <div className="scope-pick" role="listbox">
      {searchable && (
        <input
          className="q"
          autoFocus
          value={q}
          placeholder={t("搜索项目…")}
          onChange={(e) => setQ(e.target.value)}
        />
      )}
      <button
        className="fl"
        aria-pressed={onlyChanged}
        onClick={() => setOnlyChanged(!onlyChanged)}
      >
        {t("只看有例外的")}
      </button>
      {shown.map((sc) => (
        <button
          key={sc.key}
          role="option"
          aria-selected={sc.root === at}
          className="pk"
          onClick={() => onPick(sc.root)}
        >
          <span className="nm">
            {sc.label || sc.name}
            {sc.current && <i className="cur">{t("当前会话")}</i>}
          </span>
          <span className="pt">
            {sc.root}
            {sc.repo && (sc.trees ?? 1) > 1 && ` · ${t("{n} 个工作树", { n: sc.trees ?? 1 })}`}
            {!sc.repo && ` · ${t("非 git")}`}
          </span>
          {sc.overrides > 0 && <i className="ct">{t("{n} 项例外", { n: sc.overrides })}</i>}
        </button>
      ))}
      {shown.length === 0 && <div className="empty">{t("没有匹配的项目。")}</div>}
    </div>
  );
}
