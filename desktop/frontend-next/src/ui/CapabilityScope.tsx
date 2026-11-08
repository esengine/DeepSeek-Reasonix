import { useCallback, useEffect, useRef, useState, type RefObject } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import type { CapabilityScope } from "../port/port";
import { useDismiss } from "./dismiss";
import { beside, pinToViewport, useFollow, zoom } from "./place";

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

// The project the extension lists answer for, as one pill that opens the list of
// projects. It sits in settings and on the extension page, so choosing is never
// a trip to another page.
export function ScopePill({
  scope, scopes, onPick,
}: {
  scope: CapabilityScope; scopes: CapabilityScope[]; onPick: (root: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const close = useCallback(() => { setOpen(false); trigger.current?.focus({ preventScroll: true }); }, []);
  const place = useCallback(() => {
    if (!panel.current || !trigger.current) return;
    const rect = trigger.current.getBoundingClientRect();
    const width = panel.current.offsetWidth * zoom();
    pinToViewport(panel.current, rect.right - width, beside(rect, panel.current.offsetHeight * zoom(), innerHeight, "below", 4), 12);
  }, []);
  useFollow(open, place, scopes, { anchor: trigger, container: panel });
  useDismiss(open, panel, close, trigger);
  useEffect(() => {
    if (!open || !panel.current) return;
    const observer = new ResizeObserver(place);
    observer.observe(panel.current);
    return () => observer.disconnect();
  }, [open, place]);
  return (
    <span className="scopeanchor">
      <button ref={trigger} type="button" className="spill who" data-action="ext.scope" aria-haspopup="listbox" aria-expanded={open} onClick={() => setOpen(!open)}>
        {scope.label || scope.name}
        <i className="cv" aria-hidden>▾</i>
      </button>
      {open && createPortal(
        <ScopePicker
          panel={panel}
          scopes={scopes}
          at={scope.root}
          onPick={(root) => {
            close();
            onPick(root);
          }}
        />, document.body,
      )}
    </span>
  );
}

// Which folder the extension lists answer for. The shell holds several projects
// at once and the pane follows whichever one is in front, so this is chosen on
// purpose here rather than inferred from the tab in front. Worktrees of one
// repository share one answer, and saying how many is what stops "my settings
// vanished on a new branch". Choosing a project manages it without the session
// moving to it.
export function ScopeRow({
  scope, scopes, onPick,
}: {
  scope: CapabilityScope; scopes: CapabilityScope[]; onPick: (root: string) => void;
}) {
  const shared = scope.repo && (scope.trees ?? 1) > 1;
  return (
    <div className="scoperow">
      <div className="srow" data-setting="ext-scope">
        <div className="srow-meta">
          <span className="srow-name">{t("ext.manage_which_project")}</span>
          <span className="srow-desc">{t("ext.scope_hint")}</span>
        </div>
        <div className="srow-ctl">
          <ScopePill scope={scope} scopes={scopes} onPick={onPick} />
        </div>
      </div>
      <div className="scopeinfo">
        <span className="pt" title={scope.root}>{scope.root}</span>
        {scope.branch && <span className="br">{scope.branch}</span>}
        <span className="ds">
          {[
            shared ? t("ext.n_worktrees_share_this_setting", { n: scope.trees ?? 1 }) : null,
            !scope.repo ? t("ext.not_a_git_repository_kept") : null,
            scope.overrides ? t("ext.n_settings_apply_only_here", { n: scope.overrides }) : t("ext.no_exceptions_for_this_project"),
          ].filter(Boolean).join(" · ")}
        </span>
      </div>
    </div>
  );
}

// The extension page names whose list it is only when that is not the running
// project, so the same heading never quietly changes owner.
export function ScopeAway({ scope, onBack }: { scope: CapabilityScope; onBack: () => void }) {
  return (
    <div className="scopeaway" role="note">
      <span>{t("ext.managing_other_project", { name: scope.label || scope.name })}</span>
      <button type="button" className="act" data-action="ext.scope-back" onClick={onBack}>{t("ext.back_to_the_current_project")}</button>
    </div>
  );
}

// With a folder or two the list is the whole answer. Past that, the projects
// worth finding are the ones already carrying an exception — so that is the
// filter, and search only appears when the list is long enough to need it.
function ScopePicker({
  panel, scopes, at, onPick,
}: {
  panel: RefObject<HTMLDivElement | null>; scopes: CapabilityScope[]; at: string; onPick: (root: string) => void;
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
    <div ref={panel} className="scope-pick" role="listbox" aria-label={t("ext.manage_which_project")}>
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
