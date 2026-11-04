import { useState } from "react";
import { t } from "../i18n";
import type { CapabilityScope } from "../port/port";

// A switch flipped here answers for this project, not for every project. That
// is the useful default and also the invisible one, so the row that carries an
// exception says so and offers the way back — without it the same list would
// read identically in a project that simply inherited everything.
export function Exception({ onClear, busy }: { onClear: () => void; busy: boolean }) {
  return (
    <button className="exc" data-action="capability.scope" disabled={busy} title={t("ext.go_back_to_following_the")} onClick={onClear}>
      {t("ext.this_project_only_2")}
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
      {shared && <span className="sh">{t("ext.n_worktrees_share_this_setting", { n: scope.trees ?? 1 })}</span>}
      {!scope.repo && <span className="sh">{t("ext.not_a_git_repository_kept")}</span>}
      <span className="ov">
        {scope.overrides ? t("ext.n_settings_apply_only_here", { n: scope.overrides }) : t("ext.no_exceptions_for_this_project")}
      </span>
      {elsewhere && <span className="away">{t("ext.not_the_project_this_session")}</span>}
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
          placeholder={t("ext.search_projects")}
          onChange={(e) => setQ(e.target.value)}
        />
      )}
      <button
        className="fl"
        aria-pressed={onlyChanged}
        onClick={() => setOnlyChanged(!onlyChanged)}
      >
        {t("ext.only_those_with_exceptions")}
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
            {sc.current && <i className="cur">{t("common.this_session")}</i>}
          </span>
          <span className="pt">
            {sc.root}
            {sc.repo && (sc.trees ?? 1) > 1 && ` · ${t("ext.n_worktrees", { n: sc.trees ?? 1 })}`}
            {!sc.repo && ` · ${t("ext.not_git")}`}
          </span>
          {sc.overrides > 0 && <i className="ct">{t("ext.n_exceptions", { n: sc.overrides })}</i>}
        </button>
      ))}
      {shown.length === 0 && <div className="empty">{t("ext.no_project_matches")}</div>}
    </div>
  );
}
