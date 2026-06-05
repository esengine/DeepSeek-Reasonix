import { useCallback, useEffect, useId, useMemo, useState } from "react";
import { asArray } from "../lib/array";
import { app, openExternal } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { CapabilitiesView, MCPServerInput, ServerView, SkillRootSkillView, SkillRootView, SkillView } from "../lib/types";
import { InlineConfirmButton } from "./InlineConfirmButton";
import { ResizableDrawer } from "./ResizableDrawer";
import { Tooltip } from "./Tooltip";

// CapabilitiesPanel is the desktop MCP & Skills drawer — the GUI counterpart to
// the CLI's /mcp + /skill, aligning with Claude Code's Customize → Connectors:
// each server shows a connected/failed dot, transport, and tool/prompt/resource
// counts, with add / remove / retry; skills list their scope and run mode.
type CapTab = "servers" | "skills";

export function CapabilitiesPanel({
  onClose,
}: {
  onClose: () => void;
}) {
  const t = useT();
  const [view, setView] = useState<CapabilitiesView | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const [tab, setTab] = useState<CapTab>("servers");
  const [skillQuery, setSkillQuery] = useState("");
  const [expandedSkills, setExpandedSkills] = useState<Set<string>>(() => new Set());
  const [expandedErrors, setExpandedErrors] = useState<Set<string>>(() => new Set());
  const [expandedServers, setExpandedServers] = useState<Set<string>>(() => new Set());
  const [expandedServerTools, setExpandedServerTools] = useState<Set<string>>(() => new Set());

  const reload = useCallback(async () => {
    setView(normalizeCapabilitiesView(await app.Capabilities().catch(() => ({ servers: [], skills: [], skillRoots: [] }))));
  }, []);
  useEffect(() => {
    void reload();
  }, [reload]);
  useEffect(() => {
    if (tab !== "servers" || !view?.servers.some((s) => s.status === "initializing" || s.status === "deferred")) return;
    const id = window.setInterval(() => void reload(), 2500);
    return () => window.clearInterval(id);
  }, [reload, tab, view?.servers]);

  // mutate runs an MCP edit, re-reads the snapshot, and surfaces any failure as an
  // inline banner (a connect error, a missing binary, a bad URL).
  const mutate = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setErr(null);
    try {
      await fn();
      await reload();
      return true;
    } catch (e) {
      setErr(String((e as Error)?.message ?? e));
      await reload();
      return false;
    } finally {
      setBusy(false);
    }
  };

  const summary = useMemo(() => {
    if (!view) return "";
    return t("caps.summary", {
      connected: view.servers.filter((s) => s.status === "connected").length,
      failed: view.servers.filter((s) => s.status === "failed").length,
      skills: view.skills.length,
    });
  }, [view, t]);

  const filteredSkills = useMemo(() => {
    if (!view) return [];
    const q = skillQuery.trim().toLowerCase();
    if (!q) return view.skills;
    return view.skills.filter((sk) => {
      const text = [sk.name, `/${sk.name}`, sk.description, sk.scope, sk.runAs].join(" ").toLowerCase();
      return text.includes(q);
    });
  }, [view, skillQuery]);
  const skillSummary = useMemo(() => {
    if (!view) return "";
    return skillListSummary(view.skills, filteredSkills, skillQuery.trim().length > 0, t);
  }, [filteredSkills, skillQuery, t, view]);

  const serverGroups = useMemo(() => {
    const servers = sortServersForDisplay(view?.servers ?? []);
    return {
      failed: servers.filter((s) => s.status === "failed"),
      active: servers.filter((s) => s.status !== "failed"),
    };
  }, [view]);

  const toggleSkill = useCallback((name: string) => {
    setExpandedSkills((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  }, []);

  const toggleError = useCallback((name: string) => {
    setExpandedErrors((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  }, []);

  const toggleServer = useCallback((name: string) => {
    setExpandedServers((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  }, []);

  const toggleServerTools = useCallback((name: string) => {
    setExpandedServerTools((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  }, []);

  return (
    <ResizableDrawer onClose={onClose} subtle>
        <header className="drawer__head">
          <div>
            <div className="drawer__title">{t("caps.title")}</div>
            {view && <div className="drawer__summary">{summary}</div>}
          </div>
          <Tooltip label={t("common.close")}>
            <button className="chip" onClick={onClose}>
              ✕
            </button>
          </Tooltip>
        </header>

        {!view ? (
          <div className="empty">{t("caps.loading")}</div>
        ) : (
          <div className="drawer__body">
            {err && <div className="banner banner--error">{err}</div>}

            <div className="cap-tabs" role="tablist" aria-label={t("caps.title")}>
              <button
                className={`cap-tab${tab === "servers" ? " cap-tab--active" : ""}`}
                role="tab"
                aria-selected={tab === "servers"}
                onClick={() => setTab("servers")}
              >
                {t("caps.connectorsTab")}
              </button>
              <button
                className={`cap-tab${tab === "skills" ? " cap-tab--active" : ""}`}
                role="tab"
                aria-selected={tab === "skills"}
                onClick={() => setTab("skills")}
              >
                {t("caps.skillsTab")}
              </button>
            </div>

            {tab === "servers" ? (
              <section className="mem-section">
                <div className="mem-section__actions">
                  {!adding && (
                    <button className="btn btn--small" disabled={busy} onClick={() => setAdding(true)}>
                      {t("caps.addServer")}
                    </button>
                  )}
                </div>
                {serverGroups.failed.length > 0 && (
                  <FailedServersNotice
                    servers={serverGroups.failed}
                    expanded={expandedErrors}
                    onToggle={toggleError}
                    onRetry={(name) => void mutate(() => app.RetryMCPServer(name))}
                    onConfirmClearAuth={(name) => void mutate(() => app.ClearMCPServerAuthentication(name))}
                    onSetTier={(name, tier) => void mutate(() => app.SetMCPServerTier(name, tier))}
                    onConfirm={(name) => void mutate(() => app.RemoveMCPServer(name))}
                    busy={busy}
                  />
                )}
                {view.servers.length === 0 && !adding && (
                  <div className="mem-empty">{t("caps.noServers")}</div>
                )}
                <ServerGroup
                  busy={busy}
                  servers={serverGroups.active}
                  expanded={expandedServers}
                  expandedTools={expandedServerTools}
                  editing={editing}
                  onConfirm={(name) => void mutate(() => app.RemoveMCPServer(name))}
                  onEdit={(name) => {
                    setEditing(name);
                  }}
                  onCancelEdit={() => setEditing(null)}
                  onRetry={(name) => void mutate(() => app.RetryMCPServer(name))}
                  onConfirmClearAuth={(name) => void mutate(() => app.ClearMCPServerAuthentication(name))}
                  onToggle={(name, on) => void mutate(() => app.SetMCPServerEnabled(name, on))}
                  onSetTier={(name, tier) => void mutate(() => app.SetMCPServerTier(name, tier))}
                  onUpdate={(name, input) =>
                    void mutate(() => app.UpdateMCPServer(name, input)).then((ok) => {
                      if (ok) setEditing(null);
                    })
                  }
                  onToggleDetails={toggleServer}
                  onToggleTools={toggleServerTools}
                />
                {adding ? (
                  <AddServerForm busy={busy} onCancel={() => setAdding(false)} onAdd={async (input) => (await mutate(() => app.AddMCPServer(input))) && setAdding(false)} />
                ) : null}
              </section>
            ) : (
              <section className="mem-section">
                <div className="cap-search">
                  <input
                    className="mem-input"
                    type="search"
                    placeholder={t("caps.searchSkills")}
                    value={skillQuery}
                    onChange={(e) => setSkillQuery(e.target.value)}
                  />
                </div>
                <SkillSources
                  roots={view.skillRoots ?? []}
                  busy={busy}
                  onAdd={() => mutate(async () => {
                    const path = await app.PickSkillFolder();
                    if (path) await app.AddSkillPath(path);
                  })}
                  onRefresh={() => mutate(() => app.RefreshSkills())}
                  onRemove={(path) => mutate(() => app.RemoveSkillPath(path))}
                />
                <div className="cap-skills-head">
                  <div className="cap-skills-head__copy">
                    <div className="cap-skills-head__title">{t("caps.skills")}</div>
                    <div className="cap-skills-head__summary">{skillSummary}</div>
                  </div>
                </div>
                {view.skills.length === 0 ? (
                  <div className="mem-empty">{t("caps.noSkills")}</div>
                ) : filteredSkills.length === 0 ? (
                  <div className="mem-empty">{t("caps.noSkillMatches")}</div>
                ) : (
                  <div className="cap-skills">
                    {filteredSkills.map((sk) => (
                      <SkillRow
                        key={sk.name}
                        skill={sk}
                        busy={busy}
                        expanded={expandedSkills.has(sk.name)}
                        onToggle={() => toggleSkill(sk.name)}
                        onToggleEnabled={(enabled) => void mutate(() => app.SetSkillEnabled(sk.name, enabled))}
                      />
                    ))}
                  </div>
                )}
              </section>
            )}
          </div>
        )}
    </ResizableDrawer>
  );
}

function normalizeCapabilitiesView(view: CapabilitiesView | null | undefined): CapabilitiesView {
  return {
    servers: sortServersForDisplay(
      asArray(view?.servers).map((server) => ({
        ...server,
        args: asArray(server.args),
        envKeys: asArray(server.envKeys),
        toolList: asArray(server.toolList),
      })),
    ),
    skills: asArray(view?.skills),
    skillRoots: asArray(view?.skillRoots).map((root) => ({
      ...root,
      skillItems: asArray(root.skillItems),
    })),
  };
}

function sortServersForDisplay(servers: ServerView[]): ServerView[] {
  return [...servers].sort((a, b) => {
    const priority = serverDisplayPriority(a) - serverDisplayPriority(b);
    if (priority !== 0) return priority;
    return a.name.localeCompare(b.name, undefined, { sensitivity: "base" });
  });
}

function serverDisplayPriority(server: ServerView): number {
  if (server.status === "failed" || server.authStatus === "required") return 0;
  if (server.builtIn) return 1;
  if (server.status !== "disabled") return 2;
  return 3;
}

function skillListSummary(skills: SkillView[], filtered: SkillView[], searching: boolean, t: ReturnType<typeof useT>): string {
  if (searching) {
    return t("caps.skillsSummaryMatches", { matched: filtered.length, total: skills.length });
  }
  const parts = [t("caps.skillsSummaryAvailable", { skills: skills.length })];
  const scopes = ["project", "custom", "global", "builtin"];
  for (const scope of scopes) {
    const count = skills.filter((skill) => skill.scope === scope).length;
    if (count > 0) parts.push(skillScopeSummary(scope, count, t));
  }
  return parts.join(" · ");
}

function skillScopeSummary(scope: string, count: number, t: ReturnType<typeof useT>): string {
  switch (scope) {
    case "builtin":
      return t("caps.skillsSummaryBuiltin", { count });
    case "project":
      return t("caps.skillsSummaryProject", { count });
    case "custom":
      return t("caps.skillsSummaryCustom", { count });
    case "global":
      return t("caps.skillsSummaryGlobal", { count });
    default:
      return `${count} ${scope}`;
  }
}

function skillSourceSummary(active: number, missing: number, empty: number, t: ReturnType<typeof useT>): string {
  const parts: string[] = [];
  if (active > 0) parts.push(t("caps.sourcesSummaryActive", { active }));
  if (missing > 0) parts.push(t("caps.sourcesSummaryMissing", { missing }));
  if (empty > 0) parts.push(t("caps.sourcesSummaryEmpty", { empty }));
  return parts.length > 0 ? parts.join(" · ") : t("caps.sourcesSummaryNone");
}

function SkillSources({
  roots,
  busy,
  onAdd,
  onRefresh,
  onRemove,
}: {
  roots: SkillRootView[];
  busy: boolean;
  onAdd: () => void;
  onRefresh: () => void;
  onRemove: (path: string) => void;
}) {
  const t = useT();
  const [expanded, setExpanded] = useState(false);
  const [showDiagnostics, setShowDiagnostics] = useState(false);
  const [expandedRootSkills, setExpandedRootSkills] = useState<Set<string>>(() => new Set());
  const [fullRootSkills, setFullRootSkills] = useState<Set<string>>(() => new Set());
  const primaryRoots = roots.filter(isPrimarySkillRoot);
  const diagnosticRoots = roots.filter((root) => !isPrimarySkillRoot(root));
  const diagnosticsVisible = expanded && showDiagnostics;
  const shownRoots = diagnosticsVisible ? [...primaryRoots, ...diagnosticRoots] : primaryRoots;
  const summaryRoots = diagnosticsVisible ? roots : primaryRoots;
  const active = summaryRoots.filter((root) => root.skills > 0).length;
  const missing = summaryRoots.filter((root) => root.status === "missing").length;
  const empty = summaryRoots.filter((root) => root.status === "ok" && root.skills === 0).length;
  const toggleRootSkills = (key: string) => {
    setExpandedRootSkills((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };
  const toggleRootSkillFull = (key: string) => {
    setFullRootSkills((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };
  return (
    <div className={`cap-sources${expanded ? " cap-sources--expanded" : ""}`}>
      <div className="cap-sources__head">
        <div className="cap-sources__copy">
          <div className="cap-sources__title">{t("caps.sources")}</div>
          <div className="cap-sources__summary">{skillSourceSummary(active, missing, empty, t)}</div>
        </div>
        {!expanded && (
          <div className="cap-sources__actions">
            <button className="btn btn--small" type="button" onClick={() => setExpanded(true)} aria-expanded={expanded}>
              {t("caps.manageSkillSources")}
            </button>
          </div>
        )}
      </div>
      {expanded && (
        <>
          <div className="cap-sources__manage">
            <div className="cap-sources__manage-actions">
              <button className="btn btn--small" disabled={busy} onClick={onRefresh}>
                {t("caps.refreshSkills")}
              </button>
              <button className="btn btn--small" disabled={busy} onClick={onAdd}>
                {t("caps.addSkillFolder")}
              </button>
            </div>
            <button
              className="btn btn--small"
              type="button"
              onClick={() => {
                setShowDiagnostics(false);
                setExpanded(false);
              }}
              aria-expanded={expanded}
            >
              {t("common.collapse")}
            </button>
          </div>
          {shownRoots.length === 0 ? (
            <div className="mem-empty">{t("caps.noSkillRoots")}</div>
          ) : (
            <div className="cap-source-list">
              {shownRoots.map((root) => {
                const key = skillRootKey(root);
                const rootSkills = root.skillItems ?? [];
                const rootSkillsExpanded = expandedRootSkills.has(key);
                const rootSkillsFull = fullRootSkills.has(key);
                const canShowRootSkills = rootSkills.length > 0;
                const canRemoveRoot = root.scope === "custom" && root.configured;
                return (
                  <div className={`cap-source cap-source--${skillRootTone(root)}`} key={key}>
                    <span className={`cap-dot cap-dot--${skillRootDot(root)}`} />
                    <div className="cap-source__text">
                      <div className="cap-source__head">
                        <div className="cap-source__label" title={root.dir}>
                          {skillRootLabel(root)}
                        </div>
                      </div>
                      <div className="cap-source__meta">
                        <span>{skillRootStatus(root, t)}</span>
                        <span>{t("caps.skillRootCount", { skills: root.skills })}</span>
                        {root.configured && <span>{t("caps.skillRootConfigured")}</span>}
                      </div>
                      {(canShowRootSkills || canRemoveRoot) && (
                        <div className="cap-source-actions">
                          <>
                            {canShowRootSkills && (
                              <button
                                className="btn btn--small"
                                disabled={busy}
                                type="button"
                                aria-expanded={rootSkillsExpanded}
                                onClick={() => toggleRootSkills(key)}
                              >
                                {rootSkillsExpanded ? t("caps.hideSkills") : t("caps.showSkills")}
                              </button>
                              )}
                              {canRemoveRoot && (
                                <InlineConfirmButton
                                  label={t("caps.skillRootRemove")}
                                  confirmLabel={t("caps.skillRootConfirmRemove")}
                                  cancelLabel={t("common.cancel")}
                                  disabled={busy}
                                  danger
                                  onConfirm={() => onRemove(root.dir)}
                                />
                              )}
                            </>
                        </div>
                      )}
                      {rootSkillsExpanded && rootSkills.length > 0 && (
                        <SkillRootSkillsList
                          skills={rootSkills}
                          showAll={rootSkillsFull}
                          onToggleAll={() => toggleRootSkillFull(key)}
                        />
                      )}
                      {root.warning && <div className="cap-source__warning">{root.warning}</div>}
                    </div>
                    <div className="cap-source__badges">
                      {skillRootBadges(root, t).map((badge) => (
                        <span className={`cap-source-badge cap-source-badge--${badge.tone}`} key={badge.label}>
                          {badge.label}
                        </span>
                      ))}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
          {diagnosticRoots.length > 0 && (
            <button className="cap-diagnostics" type="button" onClick={() => setShowDiagnostics((v) => !v)}>
              {diagnosticsVisible ? t("caps.hideDiagnostics") : t("caps.showDiagnostics", { count: diagnosticRoots.length })}
            </button>
          )}
        </>
      )}
    </div>
  );
}

const skillRootPreviewLimit = 5;

function SkillRootSkillsList({
  skills,
  showAll,
  onToggleAll,
}: {
  skills: SkillRootSkillView[];
  showAll: boolean;
  onToggleAll: () => void;
}) {
  const t = useT();
  const visible = showAll ? skills : skills.slice(0, skillRootPreviewLimit);
  return (
    <div className="cap-source-skills">
      {visible.map((skill) => (
        <div className="cap-source-skill" key={`${skill.scope}:${skill.name}`}>
          <div className="cap-source-skill__head">
            <span className="cap-source-skill__name">/{skill.name}</span>
            <span className="cap-source-skill__badges">
              <span className={`cap-skill-badge cap-skill-badge--${skill.scope}`}>{skillScopeLabel(skill.scope, t)}</span>
              {skill.runAs === "subagent" && <span className="cap-skill-badge cap-skill-badge--run">{t("caps.subagent")}</span>}
            </span>
          </div>
          {skill.description && <div className="cap-source-skill__desc">{skill.description}</div>}
        </div>
      ))}
      {skills.length > skillRootPreviewLimit && (
        <button className="cap-source-skills__more" type="button" onClick={onToggleAll}>
          {showAll ? t("common.collapse") : t("caps.skillRootShowAllSkills", { count: skills.length })}
        </button>
      )}
    </div>
  );
}

function skillRootKey(root: SkillRootView): string {
  return `${root.scope}:${root.priority}:${root.dir}`;
}

function isPrimarySkillRoot(root: SkillRootView): boolean {
  return root.skills > 0 || root.configured || Boolean(root.warning);
}

function skillRootTone(root: SkillRootView): "active" | "empty" | "problem" {
  if (root.warning || root.status === "inactive" || root.status === "unreadable") return "problem";
  if (root.skills > 0) return "active";
  return "empty";
}

function skillRootDot(root: SkillRootView): "connected" | "disabled" | "failed" {
  const tone = skillRootTone(root);
  if (tone === "active") return "connected";
  if (tone === "empty") return "disabled";
  return "failed";
}

function skillRootStatus(root: SkillRootView, t: ReturnType<typeof useT>): string {
  if (root.status === "ok" && root.skills > 0) return t("caps.skillRootActive");
  if (root.status === "ok") return t("caps.skillRootEmpty");
  return root.status;
}

function skillRootLabel(root: SkillRootView): string {
  return root.dir;
}

function skillRootBadges(root: SkillRootView, t: ReturnType<typeof useT>): Array<{ label: string; tone: "scope" | "builtin" | "configured" | "missing" }> {
  const badges: Array<{ label: string; tone: "scope" | "builtin" | "configured" | "missing" }> = [
    { label: skillScopeLabel(root.scope, t), tone: "scope" },
    root.scope === "custom"
      ? { label: root.configured ? t("caps.skillRootUserConfigured") : t("caps.skillRootConfiguredPath"), tone: "configured" }
      : { label: t("caps.skillRootBuiltinPath"), tone: "builtin" },
  ];
  if (root.status === "missing") {
    badges.push({ label: t("caps.skillRootMissing"), tone: "missing" });
  }
  return badges;
}

function ServerGroup({
  servers,
  expanded,
  expandedTools,
  busy,
  editing,
  onConfirm,
  onEdit,
  onCancelEdit,
  onRetry,
  onConfirmClearAuth,
  onToggle,
  onSetTier,
  onUpdate,
  onToggleDetails,
  onToggleTools,
}: {
  servers: ServerView[];
  expanded: Set<string>;
  expandedTools: Set<string>;
  busy: boolean;
  editing: string | null;
  onConfirm: (name: string) => void;
  onEdit: (name: string) => void;
  onCancelEdit: () => void;
  onRetry: (name: string) => void;
  onConfirmClearAuth: (name: string) => void;
  onToggle: (name: string, on: boolean) => void;
  onSetTier: (name: string, tier: string) => void;
  onUpdate: (name: string, input: MCPServerInput) => void;
  onToggleDetails: (name: string) => void;
  onToggleTools: (name: string) => void;
}) {
  if (servers.length === 0) return null;
  return (
    <div className="cap-server-group">
      {servers.map((s) => (
        <ServerRow
          key={s.name}
          s={s}
          expanded={expanded.has(s.name)}
          toolsExpanded={expandedTools.has(s.name)}
          busy={busy}
          editing={editing === s.name}
          onConfirm={() => onConfirm(s.name)}
          onEdit={() => onEdit(s.name)}
          onCancelEdit={onCancelEdit}
          onRetry={() => onRetry(s.name)}
          onConfirmClearAuth={() => onConfirmClearAuth(s.name)}
          onToggle={(on) => onToggle(s.name, on)}
          onSetTier={(tier) => onSetTier(s.name, tier)}
          onUpdate={(input) => onUpdate(s.name, input)}
          onToggleDetails={() => onToggleDetails(s.name)}
          onToggleTools={() => onToggleTools(s.name)}
        />
      ))}
    </div>
  );
}

function FailedServersNotice({
  servers,
  expanded,
  busy,
  onToggle,
  onRetry,
  onConfirmClearAuth,
  onSetTier,
  onConfirm,
}: {
  servers: ServerView[];
  expanded: Set<string>;
  busy: boolean;
  onToggle: (name: string) => void;
  onRetry: (name: string) => void;
  onConfirmClearAuth: (name: string) => void;
  onSetTier: (name: string, tier: string) => void;
  onConfirm: (name: string) => void;
}) {
  const t = useT();
  return (
    <div className="cap-failures" role="status">
      <div className="cap-failures__head">
        <div>
          <div className="cap-failures__title">{t("caps.failureTitle", { failed: servers.length })}</div>
          <div className="cap-failures__hint">{t("caps.failureHint")}</div>
        </div>
      </div>
      <div className="cap-failures__list">
        {servers.map((s) => {
          const open = expanded.has(s.name);
          const error = s.error || t("caps.failed");
          const actionLabel = serverActionLabel(s, t);
          const canConfigure = s.configured;
          const handlePrimaryAction = () => {
            if (shouldOpenAuth(s)) {
              openExternal((s.authUrl || "").trim());
              return;
            }
            onRetry(s.name);
          };
          return (
            <div className="cap-failure" key={s.name}>
              <div className="cap-failure__main">
                <span className="cap-dot cap-dot--failed" />
                <div className="cap-failure__text">
                  <div className="cap-failure__name">{s.name}</div>
                  <div className="cap-failure__summary">{s.authStatus === "required" ? t("caps.authRequiredSummary") : summarizeServerError(error, t)}</div>
                </div>
              </div>
              <div className="cap-failure__actions">
                <button className="btn btn--small" disabled={busy} onClick={handlePrimaryAction}>
                  {actionLabel}
                </button>
                {canClearAuth(s) && (
                  <InlineConfirmButton
                    label={t("caps.clearAuth")}
                    confirmLabel={t("caps.confirmClearAuth")}
                    cancelLabel={t("common.cancel")}
                    disabled={busy}
                    onConfirm={() => onConfirmClearAuth(s.name)}
                  />
                )}
                <button className="btn btn--small" onClick={() => onToggle(s.name)} aria-expanded={open}>
                  {open ? t("common.collapse") : t("caps.showLog")}
                </button>
                {!s.builtIn && (
                  <InlineConfirmButton
                    label={t("caps.remove")}
                    confirmLabel={t("caps.confirmRemove")}
                    cancelLabel={t("common.cancel")}
                    disabled={busy}
                    danger
                    onConfirm={() => onConfirm(s.name)}
                  />
                )}
              </div>
              {canConfigure && (
                <div className="cap-failure__mode">
                  <AutoConnectControls tier={s.tier || "lazy"} busy={busy} onTierChange={(tier) => onSetTier(s.name, tier)} />
                </div>
              )}
              {open && (
                <div className="cap-failure__logbox">
                  <div className="cap-failure__logbar">
                    <span>{t("caps.rawLog")}</span>
                    <button className="btn btn--small" onClick={() => void navigator.clipboard?.writeText(error)}>
                      {t("caps.copyLog")}
                    </button>
                  </div>
                  <pre className="cap-failure__log">{error}</pre>
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

function ServerRow({
  s,
  expanded,
  toolsExpanded,
  busy,
  editing,
  onConfirm,
  onEdit,
  onCancelEdit,
  onRetry,
  onConfirmClearAuth,
  onToggle,
  onSetTier,
  onUpdate,
  onToggleDetails,
  onToggleTools,
}: {
  s: ServerView;
  expanded: boolean;
  toolsExpanded: boolean;
  busy: boolean;
  editing: boolean;
  onConfirm: () => void;
  onEdit: () => void;
  onCancelEdit: () => void;
  onRetry: () => void;
  onConfirmClearAuth: () => void;
  onToggle: (on: boolean) => void;
  onSetTier: (tier: string) => void;
  onUpdate: (input: MCPServerInput) => void;
  onToggleDetails: () => void;
  onToggleTools: () => void;
}) {
  const t = useT();
  const actionLabel = serverActionLabel(s, t);
  const tools = s.toolList ?? [];
  let sub =
    s.status === "failed"
      ? s.error || t("caps.failed")
      : s.status === "initializing"
        ? t("caps.initializing")
      : s.status === "deferred"
        ? t("caps.deferred")
      : s.status === "disabled"
        ? s.configured && !s.autoStart
          ? t("caps.disabledAutoStart")
          : t("caps.disabled")
        : t("caps.counts", { tools: s.tools, prompts: s.prompts, resources: s.resources });
  if (s.authStatus === "possible" && s.status !== "failed") {
    sub = `${sub} · ${t("caps.authPossibleShort")}`;
  }
  const enabled = s.status === "connected" || s.status === "deferred" || s.status === "initializing";
  const handlePrimaryAction = () => {
    if (shouldOpenAuth(s)) {
      openExternal((s.authUrl || "").trim());
      return;
    }
    onRetry();
  };
  return (
    <div className={`cap-server-entry${s.status === "disabled" ? " cap-server-entry--disabled" : ""}`}>
      <Tooltip label={s.error} disabled={!s.error} fill block>
        <div className={`cap-row${s.status === "disabled" ? " cap-row--disabled" : ""}`}>
          <Tooltip label={expanded ? t("caps.collapseDetails") : t("caps.expandDetails")}>
            <button
              className="cap-disclosure"
              aria-expanded={expanded}
              onClick={onToggleDetails}
            >
              {expanded ? "⌄" : "›"}
            </button>
          </Tooltip>
          <span className={`cap-dot cap-dot--${s.status}`} />
          <div className="cap-row__text">
            <div className="cap-row__head">
              <span className="cap-row__name">{s.name}</span>
              <span className="cap-row__transport">{s.transport}</span>
              {s.builtIn && <span className="cap-row__builtin">{t("caps.builtIn")}</span>}
            </div>
            <div className="cap-row__sub">{sub}</div>
          </div>
          <div className="cap-row__actions">
            {s.status === "failed" ? (
              <button className="btn btn--small" disabled={busy} onClick={handlePrimaryAction}>
                {actionLabel}
              </button>
            ) : s.status === "initializing" ? (
              <span className="cap-row__pending">{t("caps.initializingShort")}</span>
            ) : (
              <Tooltip label={enabled ? t("caps.disable") : t("caps.enable")}>
                <label className="cap-switch">
                  <input
                    type="checkbox"
                    checked={enabled}
                    disabled={busy}
                    onChange={(e) => onToggle(e.target.checked)}
                  />
                  <span className="cap-switch__track" />
                </label>
              </Tooltip>
            )}
          </div>
        </div>
      </Tooltip>
      {expanded && (
        <ServerDetails
          s={s}
          tools={tools}
          busy={busy}
          onConfirm={onConfirm}
          onConnectNow={onRetry}
          onConfirmClearAuth={onConfirmClearAuth}
          onSetTier={onSetTier}
          toolsExpanded={toolsExpanded}
          editing={editing}
          onEdit={onEdit}
          onCancelEdit={onCancelEdit}
          onUpdate={onUpdate}
          onToggleTools={onToggleTools}
        />
      )}
    </div>
  );
}

function ServerDetails({
  s,
  tools,
  busy,
  onConfirm,
  onConnectNow,
  onConfirmClearAuth,
  onSetTier,
  toolsExpanded,
  editing,
  onEdit,
  onCancelEdit,
  onUpdate,
  onToggleTools,
}: {
  s: ServerView;
  tools: ServerView["toolList"];
  busy: boolean;
  onConfirm: () => void;
  onConnectNow: () => void;
  onConfirmClearAuth: () => void;
  onSetTier: (tier: string) => void;
  toolsExpanded: boolean;
  editing: boolean;
  onEdit: () => void;
  onCancelEdit: () => void;
  onUpdate: (input: MCPServerInput) => void;
  onToggleTools: () => void;
}) {
  const t = useT();
  const command = serverCommand(s);
  const canConfigure = s.configured;
  const canEditConfig = s.configured && !s.builtIn;
  const canConnectNow = s.status === "deferred" || s.status === "disabled";
  const canShowTools = (s.tools ?? 0) > 0 || (tools?.length ?? 0) > 0;
  const showClearAuth = canClearAuth(s);
  const authLabel = serverAuthLabel(s, t);
  if (editing && canEditConfig) {
    return (
      <div className="cap-server-details">
        <EditServerForm s={s} busy={busy} onCancel={onCancelEdit} onSave={onUpdate} />
      </div>
    );
  }
  return (
    <div className="cap-server-details">
      <div className="cap-detail-grid">
        <div className="cap-detail">
          <span className="cap-detail__label">{t("caps.status")}</span>
          <span className="cap-detail__value">{serverStatusLabel(s, t)}</span>
        </div>
        <div className="cap-detail">
          <span className="cap-detail__label">{t("caps.transport")}</span>
          <span className="cap-detail__value">{s.transport}</span>
        </div>
        {authLabel && (
          <div className="cap-detail">
            <span className="cap-detail__label">{t("caps.auth")}</span>
            <span className="cap-detail__value">{authLabel}</span>
          </div>
        )}
        {canConfigure && (
          <AutoConnectControls tier={s.tier || "lazy"} busy={busy} onTierChange={onSetTier} />
        )}
        {command && (
          <div className="cap-detail cap-detail--wide">
            <span className="cap-detail__label">{s.transport === "stdio" ? t("caps.command") : t("caps.url")}</span>
            <span className="cap-detail__code">{command}</span>
          </div>
        )}
        {s.envKeys && s.envKeys.length > 0 && (
          <div className="cap-detail cap-detail--wide">
            <span className="cap-detail__label">{t("caps.envKeys")}</span>
            <span className="cap-detail__value">{s.envKeys.join(", ")}</span>
          </div>
        )}
      </div>
      <div className="cap-detail-actions">
        {canConnectNow && (
          <button className="btn btn--small" disabled={busy} onClick={onConnectNow}>
            {t("caps.connectNow")}
          </button>
        )}
        {canShowTools && (
          <button className="btn btn--small" disabled={busy} onClick={onToggleTools} aria-expanded={toolsExpanded}>
            {toolsExpanded ? t("caps.hideTools") : t("caps.showTools")}
          </button>
        )}
        {showClearAuth && (
          <InlineConfirmButton
            label={t("caps.clearAuth")}
            confirmLabel={t("caps.confirmClearAuth")}
            cancelLabel={t("common.cancel")}
            disabled={busy}
            onConfirm={onConfirmClearAuth}
          />
        )}
        {canEditConfig && (
          <>
            <button className="btn btn--small" disabled={busy} onClick={onEdit}>
              {t("caps.editConfig")}
            </button>
            <InlineConfirmButton
              label={t("caps.remove")}
              confirmLabel={t("caps.confirmRemove")}
              cancelLabel={t("common.cancel")}
              disabled={busy}
              danger
              onConfirm={onConfirm}
            />
          </>
        )}
      </div>
      {toolsExpanded && (
        tools && tools.length > 0 ? (
          <div className="cap-tool-list">
            <div className="cap-tool-list__title">{t("caps.tools")}</div>
            {tools.map((tool) => (
              <div className="cap-tool" key={tool.name}>
                <div className="cap-tool__name">{tool.name}</div>
                {tool.description && <div className="cap-tool__desc">{tool.description}</div>}
              </div>
            ))}
          </div>
        ) : (
          <div className="cap-tool-empty">{t("caps.noToolDetails")}</div>
        )
      )}
    </div>
  );
}

function EditServerForm({
  s,
  busy,
  onCancel,
  onSave,
}: {
  s: ServerView;
  busy: boolean;
  onCancel: () => void;
  onSave: (input: MCPServerInput) => void;
}) {
  const t = useT();
  const initialTransport = normalizeTransportValue(s.transport);
  const [transport, setTransport] = useState(initialTransport);
  const [command, setCommand] = useState(initialTransport === "stdio" ? serverCommand(s) : "");
  const [url, setUrl] = useState(initialTransport === "stdio" ? "" : s.url || serverCommand(s));
  const [env, setEnv] = useState("");
  const [tier, setTier] = useState(s.tier || "lazy");
  const isStdio = transport === "stdio";
  const ready = isStdio ? command.trim() !== "" : url.trim() !== "";

  const submit = () => {
    const parts = command.trim().split(/\s+/).filter(Boolean);
    const envText = env.trim();
    onSave({
      name: s.name,
      transport,
      command: isStdio ? (parts[0] ?? "") : "",
      args: isStdio ? parts.slice(1) : [],
      url: isStdio ? "" : url.trim(),
      env: envText === "" ? null : parseEnvText(envText),
      tier,
    });
  };

  return (
    <div className="cap-config-edit">
      <div className="cap-detail-grid">
        <div className="cap-detail">
          <span className="cap-detail__label">{t("caps.name")}</span>
          <span className="cap-detail__value">{s.name}</span>
        </div>
        <label className="cap-detail cap-detail--select">
          <span className="cap-detail__label">{t("caps.transport")}</span>
          <select className="mem-select" value={transport} disabled={busy} onChange={(e) => setTransport(e.target.value)}>
            <option value="stdio">stdio</option>
            <option value="http">http</option>
            <option value="sse">sse</option>
          </select>
        </label>
        {isStdio ? (
          <label className="cap-detail cap-detail--wide">
            <span className="cap-detail__label">{t("caps.command")}</span>
            <input className="mem-input" value={command} disabled={busy} onChange={(e) => setCommand(e.target.value)} placeholder={t("caps.commandPlaceholder")} />
          </label>
        ) : (
          <label className="cap-detail cap-detail--wide">
            <span className="cap-detail__label">{t("caps.url")}</span>
            <input className="mem-input" value={url} disabled={busy} onChange={(e) => setUrl(e.target.value)} placeholder={t("caps.urlPlaceholder")} />
          </label>
        )}
        <AutoConnectControls tier={tier} busy={busy} onTierChange={setTier} />
        <label className="cap-detail cap-detail--wide">
          <span className="cap-detail__label">{t("caps.envLabel")}</span>
          <textarea className="mem-textarea cap-config-edit__env" value={env} disabled={busy} onChange={(e) => setEnv(e.target.value)} placeholder={t("caps.envPlaceholder")} spellCheck={false} />
        </label>
        {s.envKeys && s.envKeys.length > 0 && (
          <div className="cap-detail cap-detail--wide">
            <span className="cap-detail__label">{t("caps.envKeys")}</span>
            <span className="cap-detail__value">{s.envKeys.join(", ")}</span>
            <span className="cap-edit-hint">{t("caps.envPreserveHint")}</span>
          </div>
        )}
      </div>
      <div className="cap-detail-actions">
        <button className="btn btn--small" disabled={busy} onClick={onCancel}>
          {t("common.cancel")}
        </button>
        <button className="btn btn--primary btn--small" disabled={busy || !ready} onClick={submit}>
          {t("caps.saveConfig")}
        </button>
      </div>
    </div>
  );
}

function AutoConnectControls({
  tier,
  busy,
  onTierChange,
}: {
  tier: string;
  busy: boolean;
  onTierChange: (tier: string) => void;
}) {
  const t = useT();
  const groupId = useId();
  const normalized = normalizeTierValue(tier);
  const options = [
    { tier: "lazy", label: t("caps.launchLazy"), hint: t("caps.launchLazyHint") },
    { tier: "background", label: t("caps.launchBackground"), hint: t("caps.launchBackgroundHint") },
    { tier: "eager", label: t("caps.launchEager"), hint: t("caps.launchEagerHint") },
  ];
  return (
    <fieldset className="cap-detail cap-detail--wide cap-connection-mode">
      <legend className="cap-detail__label">{t("caps.launchMode")}</legend>
      <div className="cap-connection-options">
        {options.map((option) => (
          <label
            className={`cap-connection-option${normalized === option.tier ? " cap-connection-option--selected" : ""}${busy ? " cap-connection-option--disabled" : ""}`}
            key={option.tier}
          >
            <input
              type="radio"
              name={`mcp-connection-tier-${groupId}`}
              value={option.tier}
              checked={normalized === option.tier}
              disabled={busy}
              onChange={() => onTierChange(option.tier)}
            />
            <span className="cap-connection-option__text">
              <span className="cap-connection-option__label">{option.label}</span>
              <span className="cap-connection-option__hint">{option.hint}</span>
            </span>
          </label>
        ))}
      </div>
    </fieldset>
  );
}

function serverCommand(s: ServerView): string {
  if (s.transport === "stdio") return [s.command, ...(s.args ?? [])].filter(Boolean).join(" ").trim();
  return (s.url || "").trim();
}

function normalizeTierValue(tier: string): string {
  return tier === "background" || tier === "eager" ? tier : "lazy";
}

function normalizeTransportValue(transport: string): string {
  return transport === "http" || transport === "sse" ? transport : "stdio";
}

function parseEnvText(env: string): Record<string, string> {
  const envMap: Record<string, string> = {};
  for (const line of env.split("\n")) {
    const eq = line.indexOf("=");
    if (eq > 0) envMap[line.slice(0, eq).trim()] = line.slice(eq + 1).trim();
  }
  return envMap;
}

function serverStatusLabel(s: ServerView, t: ReturnType<typeof useT>): string {
  switch (s.status) {
    case "connected":
      return t("caps.connected");
    case "deferred":
      return t("caps.deferred");
    case "initializing":
      return t("caps.initializing");
    case "disabled":
      return s.configured && !s.autoStart ? t("caps.disabledAutoStart") : t("caps.disabled");
    case "failed":
      if (s.authStatus === "required") return t("caps.authRequired");
      return t("caps.failed");
    default:
      return s.status;
  }
}

function summarizeServerError(error: string, t: ReturnType<typeof useT>): string {
  const normalized = error.replace(/\s+/g, " ").trim();
  const plugin = normalized.match(/plugin "([^"]+)"/i)?.[1];
  if (plugin === "codegraph" && normalized.includes("context deadline exceeded")) {
    return t("caps.codegraphWarming");
  }
  const npmCode = normalized.match(/\bnpm error code ([A-Z0-9_]+)/i)?.[1];
  const errno = normalized.match(/\berrno (-?\d+)/i)?.[1];
  const reason = npmCode
    ? `npm ${npmCode}${errno ? ` (${errno})` : ""}`
    : normalized.split(/(?:\.\s+|\n)/)[0];
  const summary = plugin ? `${plugin}: ${reason}` : reason;
  return summary.length > 180 ? `${summary.slice(0, 176).trim()}…` : summary;
}

function serverActionLabel(s: ServerView, t: ReturnType<typeof useT>): string {
  const err = (s.error || "").toLowerCase();
  if (shouldOpenAuth(s)) return t("caps.reauthorize");
  if (
    err.includes("command not found") ||
    err.includes("executable file not found") ||
    err.includes("no such file") ||
    err.includes("enoent")
  ) {
    return t("caps.checkCommand");
  }
  return t("caps.retry");
}

function serverAuthLabel(s: ServerView, t: ReturnType<typeof useT>): string {
  if (s.authStatus === "required") return t("caps.authRequired");
  if (s.authStatus === "possible") return t("caps.authPossible");
  return "";
}

function shouldOpenAuth(s: ServerView): boolean {
  const url = (s.authUrl || "").trim();
  return s.authStatus === "required" && /^https?:\/\//i.test(url);
}

function canClearAuth(s: ServerView): boolean {
  if (!s.configured || s.builtIn) return false;
  return Boolean(s.authConfigured || s.authStatus === "required" || s.authStatus === "possible" || isRemoteTransport(s.transport));
}

function isRemoteTransport(transport?: string): boolean {
  const value = (transport || "").trim().toLowerCase();
  return value === "http" || value === "streamable-http" || value === "sse";
}

function SkillRow({
  skill,
  busy,
  expanded,
  onToggle,
  onToggleEnabled,
}: {
  skill: SkillView;
  busy: boolean;
  expanded: boolean;
  onToggle: () => void;
  onToggleEnabled: (enabled: boolean) => void;
}) {
  const t = useT();
  const summary = summarizeSkillDescription(skill.description);
  const canExpand = summary !== skill.description;
  return (
    <div
      className={`cap-skill-card${expanded ? " cap-skill-card--expanded" : ""}${canExpand ? " cap-skill-card--expandable" : ""}${!skill.enabled ? " cap-skill-card--disabled" : ""}`}
    >
      <div className="cap-skill-card__top">
        <button className="cap-skill-card__toggle" type="button" onClick={onToggle} aria-expanded={expanded}>
          <span className="cap-skill-card__head">
            <span className="cap-skill-card__icon">/</span>
            <span className="cap-skill-card__main">
              <span className="cap-skill-card__command">{skill.name}</span>
              <span className="cap-skill-card__badges">
                <span className={`cap-skill-badge cap-skill-badge--${skill.scope}`}>{skillScopeLabel(skill.scope, t)}</span>
                {skill.runAs === "subagent" && <span className="cap-skill-badge cap-skill-badge--run">{t("caps.subagent")}</span>}
                {!skill.enabled && <span className="cap-skill-badge cap-skill-badge--off">{t("caps.skillDisabled")}</span>}
              </span>
            </span>
          </span>
        </button>
        <Tooltip label={skill.enabled ? t("caps.disableSkill") : t("caps.enableSkill")}>
          <label className="cap-switch">
            <input
              type="checkbox"
              checked={skill.enabled}
              disabled={busy}
              onChange={(e) => onToggleEnabled(e.target.checked)}
            />
            <span className="cap-switch__track" />
          </label>
        </Tooltip>
      </div>
      <div className="cap-skill-card__desc">{expanded ? skill.description : summary}</div>
      {canExpand && <div className="cap-skill-card__more">{expanded ? t("common.collapse") : t("common.expand")}</div>}
    </div>
  );
}

function skillScopeLabel(scope: string, t: ReturnType<typeof useT>): string {
  switch (scope) {
    case "builtin":
      return t("caps.skillScopeBuiltin");
    case "project":
      return t("caps.skillScopeProject");
    case "custom":
      return t("caps.skillScopeCustom");
    case "global":
      return t("caps.skillScopeGlobal");
    default:
      return scope;
  }
}

function summarizeSkillDescription(description: string): string {
  const normalized = description.replace(/\s+/g, " ").trim();
  if (normalized.length <= 132) return normalized;
  const sentence = normalized.match(/^.{48,132}?[。.!?；;，,]/u)?.[0]?.trim();
  if (sentence && sentence.length >= 48) return sentence.replace(/[。.!?；;，,]$/u, "");
  return `${normalized.slice(0, 128).trim()}…`;
}

function AddServerForm({
  busy,
  onCancel,
  onAdd,
}: {
  busy: boolean;
  onCancel: () => void;
  onAdd: (input: MCPServerInput) => void;
}) {
  const t = useT();
  const [name, setName] = useState("");
  const [transport, setTransport] = useState("stdio");
  const [command, setCommand] = useState("");
  const [url, setUrl] = useState("");
  const [env, setEnv] = useState("");
  const [tier, setTier] = useState("lazy");

  const isStdio = transport === "stdio";
  const ready = name.trim() !== "" && (isStdio ? command.trim() !== "" : url.trim() !== "");

  const submit = () => {
    const parts = command.trim().split(/\s+/).filter(Boolean);
    const envMap: Record<string, string> = {};
    for (const line of env.split("\n")) {
      const eq = line.indexOf("=");
      if (eq > 0) envMap[line.slice(0, eq).trim()] = line.slice(eq + 1).trim();
    }
    onAdd({
      name: name.trim(),
      transport,
      command: isStdio ? (parts[0] ?? "") : "",
      args: isStdio ? parts.slice(1) : [],
      url: isStdio ? "" : url.trim(),
      env: envMap,
      tier,
    });
  };

  return (
    <div className="prov-card prov-card--edit">
      <input className="mem-input" placeholder={t("caps.namePlaceholder")} value={name} onChange={(e) => setName(e.target.value)} />
      <label className="set-label">{t("caps.transport")}</label>
      <select className="mem-select" value={transport} onChange={(e) => setTransport(e.target.value)}>
        <option value="stdio">stdio</option>
        <option value="http">http</option>
        <option value="sse">sse</option>
      </select>
      {isStdio ? (
        <input className="mem-input" placeholder={t("caps.commandPlaceholder")} value={command} onChange={(e) => setCommand(e.target.value)} />
      ) : (
        <input className="mem-input" placeholder={t("caps.urlPlaceholder")} value={url} onChange={(e) => setUrl(e.target.value)} />
      )}
      <AutoConnectControls tier={tier} busy={busy} onTierChange={setTier} />
      <label className="set-label">{t("caps.envLabel")}</label>
      <textarea className="mem-textarea" value={env} onChange={(e) => setEnv(e.target.value)} placeholder={t("caps.envPlaceholder")} spellCheck={false} />
      <div className="prov-card__actions">
        <button className="btn btn--small" onClick={onCancel} disabled={busy}>
          {t("common.cancel")}
        </button>
        <button className="btn btn--primary btn--small" onClick={submit} disabled={busy || !ready}>
          {t("caps.add")}
        </button>
      </div>
    </div>
  );
}
