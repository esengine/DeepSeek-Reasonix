import { useCallback, useEffect, useMemo, useState } from "react";
import { ArrowLeft, ChevronDown, ChevronRight, CircleAlert, Plus, RefreshCw, Search, Server as ServerIcon } from "lucide-react";
import { asArray } from "../lib/array";
import { app, openExternal } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { mcpServerLifecycleActions, mcpServerRetryableFromAvailableList } from "../lib/mcpServerLifecycle";
import type { CapabilitiesView, MCPInstallResult, MCPMarketplaceEntry, MCPMarketplaceView, MCPServerInput, PluginAgentView, PluginCommandView, PluginCompatibilityIssue, PluginHookView, PluginInstallOptions, PluginMCPServerView, PluginSkillView, PluginView, ServerView, SkillRootSkillView, SkillRootView, SkillsSettingsView, SkillView, TabMeta } from "../lib/types";
import { InlineConfirmButton } from "./InlineConfirmButton";
import { ResizableDrawer } from "./ResizableDrawer";
import { Tooltip } from "./Tooltip";
import { ModalCloseButton } from "./ModalCloseButton";

// CapabilitiesPanel is the desktop MCP & Skills drawer — the GUI counterpart to
// the CLI's /mcp + /skill, aligning with Claude Code's Customize → Connectors:
// each server shows a connected/failed dot, transport, and tool/prompt/resource
// counts, with add / remove / retry; skills list their scope and run mode.
type CapTab = "servers" | "skills";

type SettingsSnapshot<T> = { key: string; value: T };

async function installMCPServer(input: MCPServerInput): Promise<MCPInstallResult> {
  const result = await app.InstallMCPServer(input);
  if (result.state === "issue") throw new Error(result.message);
  return result;
}

let mcpSettingsSnapshot: SettingsSnapshot<ServerView[]> | null = null;
let skillsSettingsSnapshot: SettingsSnapshot<SkillsSettingsView> | null = null;
let pluginsSettingsSnapshot: SettingsSnapshot<PluginView[]> | null = null;

function settingsSnapshotKey(meta: Awaited<ReturnType<typeof app.Meta>> | null | undefined, tabs: TabMeta[] | null | undefined): string {
  const active = tabs?.find((tab) => tab.active);
  const tabID = (active?.id || "").trim();
  const root = (active?.workspaceRoot || active?.workspacePath || active?.cwd || meta?.workspaceRoot || meta?.workspacePath || meta?.cwd || "").trim();
  const channel = (meta?.eventChannel || "").trim();
  return `${channel}|${tabID}|${root}`;
}

export function CapabilitiesPanel({
  onClose,
  initialTab = "servers",
}: {
  onClose: () => void;
  initialTab?: CapTab;
}) {
  const t = useT();
  const [view, setView] = useState<CapabilitiesView | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const [tab, setTab] = useState<CapTab>(initialTab);
  const [skillQuery, setSkillQuery] = useState("");
  const [expandedSkills, setExpandedSkills] = useState<Set<string>>(() => new Set());
  const [expandedErrors, setExpandedErrors] = useState<Set<string>>(() => new Set());
  const [expandedServers, setExpandedServers] = useState<Set<string>>(() => new Set());
  const [expandedServerTools, setExpandedServerTools] = useState<Set<string>>(() => new Set());

  const reload = useCallback(async () => {
    setView(normalizeCapabilitiesView(await app.Capabilities().catch(() => ({ servers: [], skills: [], skillRoots: [], plugins: [] }))));
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
      const text = [sk.name, `/${sk.name}`, sk.invocation, sk.plugin, sk.description, sk.scope, sk.runAs].join(" ").toLowerCase();
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
  const retryableActiveServerNames = useMemo(() => retryableAvailableServerNames(serverGroups.active), [serverGroups.active]);
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
          <div className="drawer__actions">
            <Tooltip label={t("caps.refresh")}>
              <button className="chip" disabled={busy} onClick={() => void reload()}>
                ↻
              </button>
            </Tooltip>
            <ModalCloseButton label={t("common.close")} onClick={onClose} />
          </div>
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
                <div className="cap-mcp-toolbar cap-mcp-toolbar--drawer">
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
                    onRetry={(name) => void mutate(() => app.ReconnectMCPServer(name))}
                    onRetryMany={(names) => void mutate(() => Promise.allSettled(names.map((name) => app.ReconnectMCPServer(name))))}
                    onConfirmClearAuth={(name) => void mutate(() => app.ClearMCPServerAuthentication(name))}
                    onConfirm={(name) => void mutate(() => app.RemoveMCPServer(name))}
                    onConfirmMany={(names) => void mutate(() => Promise.allSettled(names.map((name) => app.RemoveMCPServer(name))))}
                    busy={busy}
                  />
                )}
                {view.servers.length === 0 && !adding && (
                  <div className="mem-empty">{t("caps.noServers")}</div>
                )}
                {serverGroups.active.length > 0 && (
                  <div className="cap-server-section">
                    <div className="cap-server-section__head">
                      <div className="cap-server-section__title">{t("caps.availableServers")}</div>
                      <button
                        className="btn btn--small"
                        disabled={busy || retryableActiveServerNames.length === 0}
                        type="button"
                        onClick={() => void mutate(() => Promise.allSettled(retryableActiveServerNames.map((name) => app.ReconnectMCPServer(name))))}
                      >
                        {t("caps.retryAll")}
                      </button>
                    </div>
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
                      onRetry={(name) => void mutate(() => app.ReconnectMCPServer(name))}
                      onReconnect={(name) => void mutate(() => app.ReconnectMCPServer(name))}
                      onConfirmClearAuth={(name) => void mutate(() => app.ClearMCPServerAuthentication(name))}
                      onToggle={(name, on) => void mutate(() => app.SetMCPServerEnabled(name, on))}
                      onUpdate={(name, input) =>
                        void mutate(() => app.UpdateMCPServer(name, input)).then((ok) => {
                          if (ok) setEditing(null);
                        })
                      }
                      onToggleDetails={toggleServer}
                      onToggleTools={toggleServerTools}
                    />
                  </div>
                )}
                {adding ? (
                  <MCPServerSettingsEditor
                    busy={busy}
                    onCancel={() => setAdding(false)}
                    onSubmit={(input) => void mutate(() => installMCPServer(input)).then((ok) => { if (ok) setAdding(false); })}
                  />
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
    servers: normalizeServerViews(view?.servers),
    plugins: asArray(view?.plugins),
    ...normalizeSkillsSettingsView(view),
  };
}

function normalizeServerViews(servers: ServerView[] | null | undefined): ServerView[] {
  return sortServersForDisplay(
    asArray(servers).map((server) => ({
      ...server,
      args: asArray(server.args),
      envKeys: asArray(server.envKeys),
      headerKeys: asArray(server.headerKeys),
      toolList: asArray(server.toolList),
    })),
  );
}

function normalizeSkillsSettingsView(view: SkillsSettingsView | CapabilitiesView | null | undefined): SkillsSettingsView {
  return {
    skills: asArray(view?.skills),
    skillRoots: asArray(view?.skillRoots).map((root) => ({
      ...root,
      removable: Boolean(root.removable),
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

function mcpServerSummary(servers: ServerView[], t: ReturnType<typeof useT>): string {
  return t("caps.mcpSummary", {
    connected: servers.filter((s) => s.status === "connected").length,
    failed: servers.filter((s) => s.status === "failed").length,
    tools: servers.reduce((total, server) => total + (server.tools || 0), 0),
    unavailable: servers.reduce((total, server) => total + mcpServerSchemaIssueCount(server), 0),
  });
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
                const canRemoveRoot = root.removable;
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
        <div className="cap-source-skill" key={`${skill.scope}:${skill.invocation || skill.name}`}>
          <div className="cap-source-skill__head">
            <span className="cap-source-skill__name">{skill.invocation || `/${skill.name}`}</span>
            <span className="cap-source-skill__badges">
              <span className={`cap-skill-badge cap-skill-badge--${skill.scope}`}>{skillScopeLabel(skill.scope, t)}</span>
              {skill.plugin && <span className="cap-skill-badge">{t("slash.plugin", { name: skill.plugin })}</span>}
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
  onReconnect,
  onConfirmClearAuth,
  onToggle,
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
  onReconnect: (name: string) => void;
  onConfirmClearAuth: (name: string) => void;
  onToggle: (name: string, on: boolean) => void;
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
          onReconnect={() => onReconnect(s.name)}
          onConfirmClearAuth={() => onConfirmClearAuth(s.name)}
          onToggle={(on) => onToggle(s.name, on)}
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
  onRetryMany,
  onConfirmClearAuth,
  onConfirm,
  onConfirmMany,
}: {
  servers: ServerView[];
  expanded: Set<string>;
  busy: boolean;
  onToggle: (name: string) => void;
  onRetry: (name: string) => void;
  onRetryMany: (names: string[]) => void;
  onConfirmClearAuth: (name: string) => void;
  onConfirm: (name: string) => void;
  onConfirmMany: (names: string[]) => void;
}) {
  const t = useT();
  const [detailsOpen, setDetailsOpen] = useState(false);
  const [bulkOpen, setBulkOpen] = useState(false);
  const groups = useMemo(() => failureGroups(servers, t), [servers, t]);
  const removableFailures = useMemo(() => servers.filter(canBulkRemoveFailure), [servers]);
  const retryNames = useMemo(() => servers.map((s) => s.name), [servers]);
  return (
    <div className="cap-failures" role="region" aria-label={t("caps.failureTitle", { failed: servers.length })}>
      <div className="cap-failures__head">
        <div>
          <div className="cap-failures__title">{t("caps.failureTitle", { failed: servers.length })}</div>
          <div className="cap-failures__hint">{t("caps.failureHint")}</div>
        </div>
        <div className="cap-failures__actions">
          <button className="btn btn--small" disabled={busy} type="button" onClick={() => setDetailsOpen((v) => !v)} aria-expanded={detailsOpen}>
            {detailsOpen ? t("caps.hideFailureDetails") : t("caps.showFailureDetails")}
          </button>
          <button className="btn btn--small" disabled={busy || retryNames.length === 0} type="button" onClick={() => onRetryMany(retryNames)}>
            {t("caps.retryAll")}
          </button>
          {removableFailures.length > 0 && (
            <button className="btn btn--small" disabled={busy} type="button" onClick={() => setBulkOpen((v) => !v)} aria-expanded={bulkOpen}>
              {t("caps.bulkActions")}
            </button>
          )}
        </div>
      </div>
      <div className="cap-failures__meta">
        <div className="cap-failures__chips" aria-label={t("caps.failureGroups")}>
          {groups.map((group) => (
            <span className="cap-failure-chip" key={group.kind}>{group.label}</span>
          ))}
        </div>
      </div>
      {bulkOpen && removableFailures.length > 0 && (
        <div className="cap-failures__bulk">
          <InlineConfirmButton
            label={t("caps.removeInvalid", { count: removableFailures.length })}
            confirmLabel={t("caps.confirmRemoveInvalid", { count: removableFailures.length })}
            cancelLabel={t("common.cancel")}
            disabled={busy}
            danger
            onConfirm={() => onConfirmMany(removableFailures.map((s) => s.name))}
          />
        </div>
      )}
      {detailsOpen && <div className="cap-failures__list">
        {servers.map((s) => {
          const open = expanded.has(s.name);
          const error = s.error || t("caps.failed");
          const actionLabel = serverActionLabel(s, t);
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
                  <div className="cap-failure__summary">{s.authStatus === "required" ? t("caps.authRequiredSummary") : summarizeServerError(error)}</div>
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
                {!s.builtIn && !s.managedByPlugin && s.configured && (
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
      </div>}
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
  onReconnect,
  onConfirmClearAuth,
  onToggle,
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
  onReconnect: () => void;
  onConfirmClearAuth: () => void;
  onToggle: (on: boolean) => void;
  onUpdate: (input: MCPServerInput) => void;
  onToggleDetails: () => void;
  onToggleTools: () => void;
}) {
  const t = useT();
  const actionLabel = serverActionLabel(s, t);
  const lifecycle = mcpServerLifecycleActions(s);
  const tools = s.toolList ?? [];
  const schemaIssueCount = tools.filter((tool) => tool.schemaError).length;
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
  if (schemaIssueCount > 0) {
    sub = `${sub} · ${t("caps.schemaIssues", { count: schemaIssueCount })}`;
  }
  if (s.managedByPlugin) {
    sub = `${sub} · ${t("caps.managedByPlugin", { plugin: s.managedByPlugin })}`;
  }
  if (s.authStatus === "possible" && s.status !== "failed") {
    sub = `${sub} · ${t("caps.authPossibleShort")}`;
  }
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
            {lifecycle.showRetryInRow ? (
              <button className="btn btn--small" disabled={busy} onClick={handlePrimaryAction}>
                {actionLabel}
              </button>
            ) : (
              <Tooltip label={lifecycle.enabled ? t("caps.disable") : t("caps.enable")}>
                <label className="cap-switch">
                  <input
                    type="checkbox"
                    checked={lifecycle.enabled}
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
          onReconnect={onReconnect}
          onConfirmClearAuth={onConfirmClearAuth}
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
  onReconnect,
  onConfirmClearAuth,
  toolsExpanded,
  editing,
  onEdit,
  onCancelEdit,
  onUpdate,
  onToggleTools,
  standalone = false,
  showToolsToggle = true,
}: {
  s: ServerView;
  tools: ServerView["toolList"];
  busy: boolean;
  onConfirm: () => void;
  onConnectNow: () => void;
  onReconnect: () => void;
  onConfirmClearAuth: () => void;
  toolsExpanded: boolean;
  editing: boolean;
  onEdit: () => void;
  onCancelEdit: () => void;
  onUpdate: (input: MCPServerInput) => void;
  onToggleTools: () => void;
  standalone?: boolean;
  showToolsToggle?: boolean;
}) {
  const t = useT();
  const command = serverCommand(s);
  const canMutateConfig = s.configured && !s.builtIn && !s.managedByPlugin;
  const canEditConfig = canMutateConfig;
  const lifecycle = mcpServerLifecycleActions(s);
  const canConnectNow = lifecycle.canConnectNow;
  const canReconnect = lifecycle.canReconnect;
  const canShowTools = s.status === "connected" && ((s.tools ?? 0) > 0 || (tools?.length ?? 0) > 0);
  const showClearAuth = canMutateConfig && canClearAuth(s);
  const authLabel = serverAuthLabel(s, t);
  if (editing && canEditConfig) {
    return (
      <div className={`cap-server-details${standalone ? " cap-server-details--page" : ""}`}>
        <EditServerForm s={s} busy={busy} onCancel={onCancelEdit} onSave={onUpdate} />
      </div>
    );
  }
  return (
    <div className={`cap-server-details${standalone ? " cap-server-details--page" : ""}`}>
      <div className="cap-detail-grid">
        <div className="cap-detail">
          <span className="cap-detail__label">{t("caps.status")}</span>
          <span className="cap-detail__value">{serverStatusLabel(s, t)}</span>
        </div>
        {s.source && (
          <div className="cap-detail">
            <span className="cap-detail__label">{t("caps.serverSource")}</span>
            <span className="cap-detail__value">{mcpServerSourceLabel(s, t)}</span>
          </div>
        )}
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
        {s.headerKeys && s.headerKeys.length > 0 && (
          <div className="cap-detail cap-detail--wide">
            <span className="cap-detail__label">{t("caps.headerKeys")}</span>
            <span className="cap-detail__value">{s.headerKeys.join(", ")}</span>
          </div>
        )}
      </div>
      <div className="cap-detail-actions">
        {canConnectNow && (
          <button className="btn btn--small" disabled={busy} onClick={onConnectNow}>
            {t("caps.connectNow")}
          </button>
        )}
        {canReconnect && (
          <button className="btn btn--small" disabled={busy} onClick={onReconnect}>
            {t("caps.reconnect")}
          </button>
        )}
        {canShowTools && showToolsToggle && (
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
            {tools.map((tool) => {
              const unavailable = Boolean(tool.schemaError);
              return (
                <div className={`cap-tool${unavailable ? " cap-tool--unavailable" : ""}`} key={tool.name}>
                  <div className="cap-tool__name">{tool.name}</div>
                  <div className="cap-tool__desc">
                    <span>{unavailable ? tool.schemaError : tool.description}</span>
                    {unavailable ? (
                      <span className="cap-tool-hint cap-tool-hint--error" title={tool.schemaError}>
                        <CircleAlert aria-hidden size={11} strokeWidth={2.2} />
                        {t("caps.toolUnavailable")}
                      </span>
                    ) : null}
                  </div>
                </div>
              );
            })}
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
  const [headers, setHeaders] = useState("");
  const [env, setEnv] = useState("");
  const isStdio = transport === "stdio";
  const ready = isStdio ? command.trim() !== "" : url.trim() !== "";

  const submit = () => {
    const envText = env.trim();
    const headerText = headers.trim();
    onSave({
      name: s.name,
      transport,
      command: isStdio ? command.trim() : "",
      args: [],
      url: isStdio ? "" : url.trim(),
      env: envText === "" ? null : parseKeyValueText(envText),
      headers: isStdio || headerText === "" ? null : parseKeyValueText(headerText),
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
        {!isStdio && (
          <label className="cap-detail cap-detail--wide">
            <span className="cap-detail__label">{t("caps.headersLabel")}</span>
            <textarea className="mem-textarea cap-config-edit__env" value={headers} disabled={busy} onChange={(e) => setHeaders(e.target.value)} placeholder={t("caps.headersPlaceholder")} spellCheck={false} />
          </label>
        )}
        {!isStdio && s.headerKeys && s.headerKeys.length > 0 && (
          <div className="cap-detail cap-detail--wide">
            <span className="cap-detail__label">{t("caps.headerKeys")}</span>
            <span className="cap-detail__value">{s.headerKeys.join(", ")}</span>
            <span className="cap-edit-hint">{t("caps.headersPreserveHint")}</span>
          </div>
        )}
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

function serverCommand(s: ServerView): string {
  if (s.transport === "stdio") return [s.command, ...(s.args ?? [])].filter(Boolean).join(" ").trim();
  return (s.url || "").trim();
}

function normalizeTransportValue(transport: string): string {
  const value = transport.trim().toLowerCase();
  if (value === "http" || value === "streamable-http") return "http";
  if (value === "sse") return "sse";
  if (value === "" || value === "stdio") return "stdio";
  return value;
}

function parseKeyValueText(text: string): Record<string, string> {
  const values: Record<string, string> = {};
  for (const rawLine of text.split("\n")) {
    const line = rawLine.trim();
    if (!line) continue;
    const eq = line.indexOf("=");
    if (eq > 0) values[line.slice(0, eq).trim()] = line.slice(eq + 1).trim();
  }
  return values;
}

function serverStatusLabel(s: ServerView, t: ReturnType<typeof useT>): string {
  // Prefer product availability so idle enabled servers are not shown as disconnected.
  const availability = s.availability
    || (s.enabled === false || s.status === "disabled"
      ? "disabled"
      : s.status === "connected"
        ? "connected"
        : s.status === "initializing"
          ? "starting"
          : s.status === "failed"
            ? (s.authStatus === "required" ? "auth_required" : "start_failed")
            : s.status === "deferred"
              ? "available_on_demand"
              : s.status);
  switch (availability) {
    case "connected":
      return t("caps.connected");
    case "available_on_demand":
      return t("caps.deferred");
    case "starting":
      return t("caps.initializing");
    case "disabled":
      return t("caps.disabled");
    case "auth_required":
      return t("caps.authRequired");
    case "project_auth_changed":
      return t("caps.projectAuthChanged");
    case "start_failed":
      return t("caps.failed");
    default:
      switch (s.status) {
        case "connected":
          return t("caps.connected");
        case "deferred":
          return t("caps.deferred");
        case "initializing":
          return t("caps.initializing");
        case "disabled":
          return t("caps.disabled");
        case "failed":
          if (s.authStatus === "required") return t("caps.authRequired");
          return t("caps.failed");
        default:
          return s.status;
      }
  }
}

export function summarizeServerError(error: string): string {
  const normalized = error.replace(/\s+/g, " ").trim();
  const plugin = normalized.match(/plugin "([^"]+)"/i)?.[1];
  const npmCode = normalized.match(/\bnpm (?:error|ERR!) code ([A-Z0-9_]+)/i)?.[1];
  const errno = normalized.match(/\berrno (-?\d+)/i)?.[1];
  const networkContext = npmCode ? npmNetworkContext(normalized, npmCode) : "";
  const reason = npmCode
    ? `npm ${npmCode}${errno ? ` (${errno})` : ""}${networkContext}`
    : normalized.split(/(?:\.\s+|\n)/)[0];
  const summary = plugin ? `${plugin}: ${reason}` : reason;
  return summary.length > 180 ? `${summary.slice(0, 176).trim()}…` : summary;
}

function npmNetworkContext(error: string, code: string): string {
  if (!/^(?:ECONNREFUSED|ECONNRESET|ENETUNREACH|ETIMEDOUT|EAI_AGAIN|ENOTFOUND)$/i.test(code)) return "";

  let registry = "";
  const requestURL = error.match(/\brequest to (https?:\/\/[^\s]+)/i)?.[1]?.replace(/[),.;]+$/, "");
  if (requestURL) {
    try {
      registry = new URL(requestURL).host;
    } catch {
      registry = "";
    }
  }

  let endpoint = error.match(
    /\b(?:connect\s+)?(?:ECONNREFUSED|ECONNRESET|ENETUNREACH|ETIMEDOUT|EAI_AGAIN|ENOTFOUND)\s+((?:\[[0-9a-f:]+\]|[a-z0-9._-]+):\d{1,5})\b/i,
  )?.[1] ?? "";
  if (!endpoint) {
    const address = error.match(/\baddress\s+([^\s,;]+)/i)?.[1];
    const port = error.match(/\bport\s+(\d{1,5})\b/i)?.[1];
    if (address && port) endpoint = `${address}:${port}`;
  }

  if (registry && endpoint && registry.toLowerCase() !== endpoint.toLowerCase()) return ` · ${registry} → ${endpoint}`;
  if (registry || endpoint) return ` · ${registry || endpoint}`;
  return "";
}

export type FailureKind = "auth" | "missing-command" | "command-unavailable" | "network" | "other";

export function failureKind(server: ServerView): FailureKind {
  if (server.authStatus === "required") return "auth";
  const err = (server.error || "").toLowerCase();
  if (err.includes("command is required")) return "missing-command";
  if (
    err.includes("command not found") ||
    err.includes("executable file not found") ||
    err.includes("no such file") ||
    err.includes("enoent")
  ) {
    return "command-unavailable";
  }
  if (
    err.includes("401") ||
    err.includes("403") ||
    err.includes("unauthorized") ||
    err.includes("forbidden") ||
    err.includes("timeout") ||
    err.includes("network") ||
    err.includes("econnrefused") ||
    err.includes("econnreset") ||
    err.includes("enetunreach") ||
    err.includes("etimedout") ||
    err.includes("eai_again") ||
    err.includes("enotfound")
  ) {
    return "network";
  }
  return "other";
}

function failureGroups(servers: ServerView[], t: ReturnType<typeof useT>): Array<{ kind: FailureKind; label: string }> {
  const counts = new Map<FailureKind, number>();
  for (const server of servers) {
    const kind = failureKind(server);
    counts.set(kind, (counts.get(kind) ?? 0) + 1);
  }
  const order: FailureKind[] = ["missing-command", "command-unavailable", "auth", "network", "other"];
  return order.flatMap((kind) => {
    const count = counts.get(kind) ?? 0;
    if (count === 0) return [];
    return [{ kind, label: failureGroupLabel(kind, count, t) }];
  });
}

function failureGroupLabel(kind: FailureKind, count: number, t: ReturnType<typeof useT>): string {
  switch (kind) {
    case "auth":
      return t("caps.failureGroupAuth", { count });
    case "missing-command":
      return t("caps.failureGroupMissingCommand", { count });
    case "command-unavailable":
      return t("caps.failureGroupCommandUnavailable", { count });
    case "network":
      return t("caps.failureGroupNetwork", { count });
    default:
      return t("caps.failureGroupOther", { count });
  }
}

function canBulkRemoveFailure(server: ServerView): boolean {
  if (server.builtIn || server.managedByPlugin || !server.configured) return false;
  const kind = failureKind(server);
  return kind === "missing-command" || kind === "command-unavailable";
}

function retryableAvailableServerNames(servers: ServerView[]): string[] {
  return servers.filter(mcpServerRetryableFromAvailableList).map((s) => s.name);
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
  if (!s.configured || s.builtIn || s.managedByPlugin) return false;
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
              <span className="cap-skill-card__command">{(skill.invocation || `/${skill.name}`).replace(/^\//, "")}</span>
              <span className="cap-skill-card__badges">
                <span className={`cap-skill-badge cap-skill-badge--${skill.scope}`}>{skillScopeLabel(skill.scope, t)}</span>
                {skill.plugin && <span className="cap-skill-badge">{t("slash.plugin", { name: skill.plugin })}</span>}
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
      {canExpand && (
        <button className="cap-skill-card__more" type="button" onClick={onToggle} aria-expanded={expanded}>
          {expanded ? t("common.collapse") : t("common.expand")}
        </button>
      )}
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

function tokenizeMCPCommand(raw: string): string[] {
	const tokens: string[] = [];
	let token = "";
	let quote = "";
	for (let i = 0; i < raw.length; i += 1) {
		const ch = raw[i];
		if (quote) {
			if (ch === quote) {
				quote = "";
				continue;
			}
			if (ch === "\\" && quote === '"' && i + 1 < raw.length && /["\\]/.test(raw[i + 1])) {
				token += raw[i + 1];
				i += 1;
				continue;
			}
			token += ch;
			continue;
		}
		if (ch === '"' || ch === "'") {
			quote = ch;
			continue;
		}
		if (ch === "\\" && i + 1 < raw.length && /\s/.test(raw[i + 1])) {
			token += raw[i + 1];
			i += 1;
			continue;
		}
		if (/\s/.test(ch)) {
			if (token) tokens.push(token);
			token = "";
			continue;
		}
		token += ch;
	}
	if (token) tokens.push(token);
	return tokens;
}

function firstMCPCommandOperand(args: string[]): string {
	const valueFlags = new Set(["-p", "--package", "-c", "--call", "--node-options", "--python"]);
	let options = true;
	for (let i = 0; i < args.length; i += 1) {
		const arg = args[i];
		if (options && arg === "--") {
			options = false;
			continue;
		}
		if (options && arg.startsWith("-")) {
			if (valueFlags.has(arg)) i += 1;
			continue;
		}
		return arg;
	}
	return "";
}

function quickMCPName(raw: string): string {
	const argv = tokenizeMCPCommand(raw);
	const executable = argv[0]?.split(/[\\/]/).pop()?.toLowerCase().replace(/\.(?:cmd|exe|bat)$/i, "") || "";
	let candidate = argv[0] || "mcp-server";
	if (["npx", "bunx", "uvx"].includes(executable)) {
		candidate = firstMCPCommandOperand(argv.slice(1)) || candidate;
	} else if (["python", "python3", "py"].includes(executable)) {
		const moduleIndex = argv.findIndex((arg) => arg === "-m");
		candidate = moduleIndex >= 0 ? argv[moduleIndex + 1] || candidate : firstMCPCommandOperand(argv.slice(1)) || candidate;
	} else if (executable === "node") {
		candidate = firstMCPCommandOperand(argv.slice(1)) || candidate;
	} else if (executable === "uv" && argv[1] === "run") {
		candidate = firstMCPCommandOperand(argv.slice(2)) || candidate;
	}
	const base = candidate.split(/[\\/]/).pop() || candidate;
	const unversioned = base.replace(/@[^@]+$/, "").replace(/\.(?:cmd|exe|bat)$/i, "");
	const sanitized = unversioned.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
	return sanitized && /[a-z0-9]/.test(sanitized) && !["npx", "uvx", "uv", "node", "bunx", "python", "python3", "py"].includes(sanitized)
		? sanitized
		: "mcp-server";
}

export function parseMCPQuickDefinition(raw: string): MCPServerInput {
  const definition = raw.trim();
  if (definition.startsWith("{")) return parseMCPServerJSON(definition).input;
  if (/^https?:\/\//i.test(definition)) {
    let name = "remote-mcp";
    try {
      name = new URL(definition).hostname.replace(/^www\./, "").split(".")[0] || name;
    } catch {
      throw new Error("invalid" satisfies MCPServerJSONError);
    }
    return { name, transport: "http", command: "", args: [], url: definition, env: null, headers: null };
  }
  return { name: quickMCPName(definition), transport: "stdio", command: definition, args: [], url: "", env: null, headers: null };
}

type PluginInstallPlanAction = {
  action?: string;
  kind?: string;
  name?: string;
  source?: string;
  status?: string;
  message?: string;
  error?: string;
  compatibility?: string;
  mappedCapabilities?: string[];
  skippedCapabilities?: PluginCompatibilityIssue[];
  agentCount?: number;
  skillCount?: number;
  commandCount?: number;
  hookCount?: number;
  toolCount?: number;
};

type PluginInstallPlanView = {
  raw: string;
  ok?: boolean;
  status?: string;
  name?: string;
  actions: PluginInstallPlanAction[];
  warnings: string[];
  error?: string;
};

type PluginInstallMode = "local" | "git";

// PluginsSettingsPage is the desktop plugin package manager embedded inside
// Settings. It mirrors the MCP/Skills density: install planning on top, package
// rows below, and diagnostics/details only when a row is expanded.
export function PluginsSettingsPage() {
	const t = useT();
	const [snapshotKey, setSnapshotKey] = useState("");
	const [plugins, setPlugins] = useState<PluginView[] | null>(null);
	const [busy, setBusy] = useState(false);
	const [err, setErr] = useState<string | null>(null);
	const [installMode, setInstallMode] = useState<PluginInstallMode>("local");
	const [localSource, setLocalSource] = useState("");
	const [gitSource, setGitSource] = useState("");
	const [name, setName] = useState("");
	const [link, setLink] = useState(false);
	const [replace, setReplace] = useState(false);
	const [plan, setPlan] = useState<PluginInstallPlanView | null>(null);
	const [notice, setNotice] = useState<string | null>(null);
	const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
	const [diagnostics, setDiagnostics] = useState<Record<string, PluginView>>({});

	const reload = useCallback(async () => {
		const [meta, tabs] = await Promise.all([
			app.Meta().catch(() => null),
			app.ListTabs().catch(() => []),
		]);
		const key = settingsSnapshotKey(meta, tabs);
		setSnapshotKey(key);
		const cached = key ? pluginsSettingsSnapshot : null;
		if (cached?.key === key) {
			setPlugins(cached.value);
		} else {
			setPlugins(null);
		}
		const next = normalizePluginViews(await app.Plugins().catch(() => []));
		pluginsSettingsSnapshot = { key, value: next };
		setPlugins(next);
	}, []);
	useEffect(() => { void reload(); }, [reload]);

	const run = async (fn: () => Promise<unknown>, reloadAfter = true) => {
		setBusy(true);
		setErr(null);
		setNotice(null);
		try {
			const result = await fn();
			if (typeof result === "string" && result.trim()) {
				const parsed = parsePluginInstallPlan(result);
				setNotice(pluginPlanNotice(parsed, t));
			}
			if (reloadAfter) await reload();
			return true;
		} catch (e) {
			setErr(String((e as Error)?.message ?? e));
			if (reloadAfter) await reload();
			return false;
		} finally {
			setBusy(false);
		}
	};

	const sourceValue = (installMode === "local" ? localSource : gitSource).trim();
	const installOptions = (): PluginInstallOptions => ({
		dryRun: false,
		link: installMode === "local" ? link : false,
		replace,
		name: installMode === "git" ? name.trim() || undefined : undefined,
	});
	const actionBusy = busy || !snapshotKey || !plugins;
	const canPlan = sourceValue.length > 0 && !actionBusy;
	const summary = plugins ? pluginListSummary(plugins, t) : "";
	const togglePlugin = useCallback((pluginName: string) => {
		setExpanded((prev) => { const next = new Set(prev); if (next.has(pluginName)) next.delete(pluginName); else next.add(pluginName); return next; });
	}, []);
	const setMode = (mode: PluginInstallMode) => {
		setInstallMode(mode);
		setPlan(null);
	};
	const previewInstall = () => {
		if (!sourceValue) return;
		void run(async () => {
			const raw = await app.PlanPluginInstall(sourceValue, { ...installOptions(), dryRun: true });
			setPlan(parsePluginInstallPlan(raw));
		}, false);
	};
	const install = () => {
		if (!sourceValue) return;
		void run(async () => {
			const raw = await app.InstallPlugin(sourceValue, installOptions());
			setPlan(parsePluginInstallPlan(raw));
			return raw;
		});
	};
	const runDoctor = (pluginName: string) => {
		void run(async () => {
			const view = normalizePluginView(await app.PluginDoctor(pluginName));
			setDiagnostics((prev) => ({ ...prev, [pluginName]: view }));
			setExpanded((prev) => {
				const next = new Set(prev);
				next.add(pluginName);
				return next;
			});
		}, false);
	};
	const updateLocalSource = (value: string) => {
		setLocalSource(value);
		setPlan(null);
	};
	const updateGitSource = (value: string) => {
		setGitSource(value);
		setPlan(null);
	};
	const pickPluginFolder = () => {
		void run(async () => {
			const path = await app.PickPluginFolder();
			if (path) {
				setInstallMode("local");
				updateLocalSource(path);
			}
		}, false);
	};

	return (
		<section className="mem-section">
			{err && <div className="banner banner--error">{err}</div>}
			{notice && !err && <div className="banner banner--success">{notice}</div>}
			<div className="cap-plugin-installer">
				<div className="cap-plugin-installer__head">
					<div className="cap-plugin-installer__copy">
						<div className="cap-plugin-installer__title">{t("caps.pluginInstallTitle")}</div>
						<div className="cap-plugin-installer__hint">{t("caps.pluginInstallHint")}</div>
					</div>
					<div className="cap-tabs cap-plugin-installer__mode" role="group" aria-label={t("caps.pluginInstallMethod")}>
						<button
							className={`cap-tab${installMode === "local" ? " cap-tab--active" : ""}`}
							type="button"
							aria-pressed={installMode === "local"}
							onClick={() => setMode("local")}
						>
							{t("caps.pluginInstallLocal")}
						</button>
						<button
							className={`cap-tab${installMode === "git" ? " cap-tab--active" : ""}`}
							type="button"
							aria-pressed={installMode === "git"}
							onClick={() => setMode("git")}
						>
							{t("caps.pluginInstallGit")}
						</button>
					</div>
				</div>
				<div className="cap-plugin-form-grid">
					{installMode === "local" ? (
						<div className="cap-plugin-fields cap-plugin-fields--local">
							<div className="cap-plugin-folder-field">
								<button className="btn btn--small" disabled={actionBusy} type="button" onClick={pickPluginFolder}>
									{t("caps.pluginChooseLocalFolder")}
								</button>
								<div
									className={`cap-plugin-path${localSource ? "" : " cap-plugin-path--empty"}`}
									aria-label={t("caps.pluginLocalFolder")}
								>
									{localSource || t("caps.pluginNoLocalFolder")}
								</div>
							</div>
						</div>
					) : (
						<div className="cap-plugin-fields cap-plugin-fields--git">
							<input
								className="mem-input"
								aria-label={t("caps.pluginGitSource")}
								placeholder={t("caps.pluginSourcePlaceholder")}
								value={gitSource}
								onInput={(e) => updateGitSource(e.currentTarget.value)}
								onChange={(e) => updateGitSource(e.target.value)}
							/>
							<div className="cap-plugin-field">
								<input
									className="mem-input"
									aria-label={t("caps.pluginInstallName")}
									placeholder={t("caps.pluginInstallNamePlaceholder")}
									value={name}
									onChange={(e) => setName(e.target.value)}
								/>
							</div>
						</div>
					)}
					<div className="cap-plugin-installer__options">
						<div className="cap-plugin-option-block">
							<label className="cap-plugin-option">
								<input type="checkbox" checked={replace} disabled={actionBusy} onChange={(e) => setReplace(e.target.checked)} />
								<span>{t("caps.pluginReplace")}</span>
							</label>
							<div className="cap-plugin-option-hint">{t("caps.pluginReplaceHint")}</div>
						</div>
						{installMode === "local" && (
							<div className="cap-plugin-option-block">
								<label className="cap-plugin-option">
									<input type="checkbox" checked={link} disabled={actionBusy} onChange={(e) => setLink(e.target.checked)} />
									<span>{t("caps.pluginLink")}</span>
								</label>
								<div className="cap-plugin-option-hint">{t("caps.pluginLinkHint")}</div>
							</div>
						)}
					</div>
					<div className="cap-plugin-installer__actions">
						<button className="btn btn--small" type="button" disabled={!canPlan} onClick={previewInstall}>
							{t("caps.pluginPreview")}
						</button>
						<button className="btn btn--primary btn--small" type="button" disabled={!canPlan} onClick={install}>
							{t("caps.pluginInstall")}
						</button>
					</div>
				</div>
			</div>
			{plan && <PluginPlanPreview plan={plan} />}
			<div className="cap-server-section cap-plugin-section">
				<div className="cap-server-section__head">
					<div className="cap-server-section__copy">
						<div className="cap-server-section__title">{t("caps.installedPlugins")}</div>
						{plugins && plugins.length > 0 && <div className="drawer__summary">{summary}</div>}
					</div>
					<button className="btn btn--small" disabled={actionBusy} type="button" onClick={() => void reload()}>
						{t("caps.pluginRefresh")}
					</button>
				</div>
				{!plugins ? (
					<div className="mem-empty">{t("caps.loading")}</div>
				) : plugins.length === 0 ? (
					<div className="mem-empty mem-empty--cta">
						<strong>{t("caps.noPluginsTitle")}</strong>
						<span>{t("caps.noPluginsHint")}</span>
					</div>
				) : (
					<div className="cap-server-group">
						{plugins.map((plugin) => (
							<PluginRow
								key={plugin.name}
								plugin={plugin}
								diagnostic={diagnostics[plugin.name]}
								busy={actionBusy}
								expanded={expanded.has(plugin.name)}
								onToggleDetails={() => togglePlugin(plugin.name)}
								onToggleEnabled={(enabled) => void run(() => app.SetPluginEnabled(plugin.name, enabled))}
								onUpdate={() => void run(() => app.UpdatePlugin(plugin.name))}
								onDoctor={() => runDoctor(plugin.name)}
								onRemove={() => void run(() => app.RemovePlugin(plugin.name))}
							/>
						))}
					</div>
				)}
			</div>
		</section>
	);
}

function PluginPlanPreview({ plan }: { plan: PluginInstallPlanView }) {
	const t = useT();
	return (
		<div className={`cap-plugin-plan${plan.error ? " cap-plugin-plan--error" : ""}`}>
			<div className="cap-plugin-plan__head">
				<div className="cap-plugin-plan__title">{plan.error ? t("caps.pluginPlanError") : t("caps.pluginPlanReady")}</div>
				{plan.status && <span className="cap-source-badge">{plan.status}</span>}
			</div>
			{plan.name && <div className="cap-plugin-plan__meta">{plan.name}</div>}
			{plan.error && <div className="cap-plugin-plan__warning">{plan.error}</div>}
			{plan.warnings.map((warning, idx) => (
				<div className="cap-plugin-plan__warning" key={`${warning}-${idx}`}>{warning}</div>
			))}
			{plan.actions.length > 0 ? (
				<div className="cap-plugin-actions">
					{plan.actions.map((action, idx) => (
						<div className="cap-plugin-action" key={`${action.action || action.kind || "action"}-${idx}`}>
							<span className="cap-plugin-action__name">{pluginPlanActionLabel(action, t)}</span>
							{action.status && <span className="cap-source-badge">{action.status}</span>}
							{action.compatibility && <span className="cap-source-badge">{pluginCompatibilityLabel(action.compatibility, t)}</span>}
							{action.source && <span className="cap-plugin-action__source">{action.source}</span>}
							{asArray(action.mappedCapabilities).length > 0 && <span className="cap-plugin-action__source">{t("caps.pluginMappedCapabilities", { capabilities: asArray(action.mappedCapabilities).join(", ") })}</span>}
							{asArray(action.skippedCapabilities).map((issue, issueIndex) => <span className="cap-plugin-plan__warning" key={`${issue.capability}-${issue.path || ""}-${issueIndex}`}>{issue.capability}: {issue.reason}</span>)}
							{action.message && <span className="cap-plugin-action__source">{action.message}</span>}
							{action.error && <span className="cap-plugin-plan__warning">{action.error}</span>}
						</div>
					))}
				</div>
			) : (
				<pre className="cap-plugin-plan__raw">{plan.raw}</pre>
			)}
		</div>
	);
}

function PluginRow({
	plugin,
	diagnostic,
	busy,
	expanded,
	onToggleDetails,
	onToggleEnabled,
	onUpdate,
	onDoctor,
	onRemove,
}: {
	plugin: PluginView;
	diagnostic?: PluginView;
	busy: boolean;
	expanded: boolean;
	onToggleDetails: () => void;
	onToggleEnabled: (enabled: boolean) => void;
	onUpdate: () => void;
	onDoctor: () => void;
	onRemove: () => void;
}) {
	const t = useT();
	const status = plugin.error ? "failed" : plugin.enabled ? "connected" : "disabled";
	const warnings = pluginWarnings(plugin, diagnostic);
	const sub = plugin.error || pluginCapabilitiesSummary(plugin, t);
	return (
		<div className={`cap-server-entry cap-plugin-entry${plugin.enabled ? "" : " cap-server-entry--disabled"}`}>
			<Tooltip label={plugin.error} disabled={!plugin.error} fill block>
				<div className={`cap-row${plugin.enabled ? "" : " cap-row--disabled"}`}>
					<Tooltip label={expanded ? t("caps.collapseDetails") : t("caps.expandDetails")}>
						<button
							className="cap-disclosure"
							aria-expanded={expanded}
							type="button"
							onClick={onToggleDetails}
						>
							{expanded ? "⌄" : "›"}
						</button>
					</Tooltip>
					<span className={`cap-dot cap-dot--${status}`} />
					<div className="cap-row__text">
						<div className="cap-row__head">
							<span className="cap-row__name">{plugin.name}</span>
							{plugin.manifestKind && <span className="cap-row__transport">{plugin.manifestKind}</span>}
							{plugin.compatibility && <span className="cap-source-badge">{pluginCompatibilityLabel(plugin.compatibility, t)}</span>}
							{plugin.version && <span className="cap-source-badge">{plugin.version}</span>}
							{warnings.length > 0 && <span className="cap-row__update cap-row__update--error">{t("caps.pluginWarnings", { count: warnings.length })}</span>}
						</div>
						<div className="cap-row__sub">{sub}</div>
					</div>
					<div className="cap-row__actions">
						<Tooltip label={plugin.enabled ? t("caps.pluginDisable") : t("caps.pluginEnable")}>
							<label className="cap-switch">
								<input
									type="checkbox"
									checked={plugin.enabled}
									disabled={busy}
									onChange={(e) => onToggleEnabled(e.target.checked)}
								/>
								<span className="cap-switch__track" />
							</label>
						</Tooltip>
					</div>
				</div>
			</Tooltip>
			{expanded && (
				<div className="cap-server-details">
					<div className="cap-detail-grid">
						<div className="cap-detail">
							<span className="cap-detail__label">{t("caps.status")}</span>
							<span className="cap-detail__value">{plugin.enabled ? t("caps.pluginEnabled") : t("caps.pluginDisabled")}</span>
						</div>
						{plugin.version && (
							<div className="cap-detail">
								<span className="cap-detail__label">{t("caps.pluginVersion")}</span>
								<span className="cap-detail__value">{plugin.version}</span>
							</div>
						)}
						{plugin.source && (
							<div className="cap-detail cap-detail--wide">
								<span className="cap-detail__label">{t("caps.pluginSource")}</span>
								<span className="cap-detail__code">{plugin.source}</span>
							</div>
						)}
						{plugin.root && (
							<div className="cap-detail cap-detail--wide">
								<span className="cap-detail__label">{t("caps.pluginRoot")}</span>
								<span className="cap-detail__code">{plugin.root}</span>
							</div>
						)}
					</div>
					{plugin.description && <div className="cap-plugin-description">{plugin.description}</div>}
					{asArray(plugin.mappedCapabilities).length > 0 && <div className="cap-plugin-description">{t("caps.pluginMappedCapabilities", { capabilities: asArray(plugin.mappedCapabilities).join(", ") })}</div>}
					<PluginUsageDetails plugin={plugin} />
					{asArray(plugin.skippedCapabilities).map((issue, idx) => (
						<div className="cap-source__warning" key={`${issue.capability}-${issue.path || ""}-${idx}`}>{t("caps.pluginSkippedCapability", { capability: issue.capability, reason: issue.reason })}</div>
					))}
					{diagnostic?.error && <div className="cap-source__warning">{diagnostic.error}</div>}
					{warnings.map((warning, idx) => (
						<div className="cap-source__warning" key={`${plugin.name}-warning-${idx}`}>{warning}</div>
					))}
					<div className="cap-detail-actions">
						<button className="btn btn--small" disabled={busy} type="button" onClick={onUpdate}>
							{t("caps.pluginUpdate")}
						</button>
						<button className="btn btn--small" disabled={busy} type="button" onClick={onDoctor}>
							{t("caps.pluginDoctor")}
						</button>
						<InlineConfirmButton
							label={t("caps.pluginRemove")}
							confirmLabel={t("caps.pluginConfirmRemove")}
							cancelLabel={t("common.cancel")}
							disabled={busy}
							danger
							onConfirm={onRemove}
						/>
					</div>
				</div>
			)}
		</div>
	);
}

function PluginUsageDetails({ plugin }: { plugin: PluginView }) {
	const t = useT();
	const skills = asArray(plugin.skillDetails);
	const agents = asArray(plugin.agentDetails);
	const commands = asArray(plugin.commandDetails);
	const hooks = asArray(plugin.hookDetails);
	const mcps = asArray(plugin.mcpServerDetails);
	const hasDetails = skills.length > 0 || agents.length > 0 || commands.length > 0 || hooks.length > 0 || mcps.length > 0;
	return (
		<div className="cap-plugin-usage">
			<div className="cap-plugin-usage__title">{t("caps.pluginUsageTitle")}</div>
			<div className="cap-plugin-usage__hint">
				{plugin.enabled ? t("caps.pluginUsageEnabledHint") : t("caps.pluginUsageDisabledHint")}
			</div>
			{hasDetails ? (
				<div className="cap-plugin-capabilities">
					{commands.length > 0 && <PluginCommandList commands={commands} />}
					{skills.length > 0 && <PluginSkillList skills={skills} />}
					{agents.length > 0 && <PluginAgentList agents={agents} />}
					{hooks.length > 0 && <PluginHookList hooks={hooks} />}
					{mcps.length > 0 && <PluginMCPList servers={mcps} />}
				</div>
			) : (
				<div className="cap-plugin-usage__empty">{t("caps.pluginNoCapabilityDetails")}</div>
			)}
		</div>
	);
}

function PluginAgentList({ agents }: { agents: PluginAgentView[] }) {
	const t = useT();
	return (
		<div className="cap-plugin-capability">
			<div className="cap-plugin-capability__head">{t("caps.pluginAgentsTitle")}</div>
			<div className="cap-plugin-capability__hint">{t("caps.pluginAgentsHint")}</div>
			<div className="cap-plugin-capability__list">
				{agents.map((agent) => (
					<div className="cap-plugin-capability__item" key={`${agent.name}-${agent.path || ""}`}>
						<div className="cap-plugin-capability__line">
							<span className="cap-plugin-capability__name">{agent.invocation || agent.name}</span>
							{agent.model && <span className="cap-source-badge">{agent.model}</span>}
						</div>
						<div className="cap-plugin-capability__desc">{agent.description || t("caps.pluginNoDescription")}</div>
					</div>
				))}
			</div>
		</div>
	);
}

function PluginCommandList({ commands }: { commands: PluginCommandView[] }) {
	const t = useT();
	return (
		<div className="cap-plugin-capability">
			<div className="cap-plugin-capability__head">{t("caps.pluginCommandsTitle")}</div>
			<div className="cap-plugin-capability__hint">{t("caps.pluginCommandsHint")}</div>
			<div className="cap-plugin-capability__list">
				{commands.map((command) => (
					<div className="cap-plugin-capability__item" key={`${command.name}-${command.path || command.invocation || ""}`}>
						<div className="cap-plugin-capability__line">
							<span className="cap-plugin-capability__name">{command.invocation || `/${command.name}`}</span>
							{command.argHint && <span className="cap-source-badge">{command.argHint}</span>}
							{command.shadowed && <span className="cap-source-badge">{t("caps.pluginCommandShadowed")}</span>}
						</div>
						<div className="cap-plugin-capability__desc">{command.description || t("caps.pluginNoDescription")}</div>
						{command.shadowed && (
							<div className="cap-plugin-capability__hint">
								{command.shadowedByPlugin
									? t("caps.pluginCommandQualifiedOccupiedByPlugin", { plugin: command.shadowedByPlugin })
									: t("caps.pluginCommandQualifiedOccupiedByCustom")}
							</div>
						)}
					</div>
				))}
			</div>
		</div>
	);
}

function PluginSkillList({ skills }: { skills: PluginSkillView[] }) {
	const t = useT();
	return (
		<div className="cap-plugin-capability">
			<div className="cap-plugin-capability__head">{t("caps.pluginSkillsTitle")}</div>
			<div className="cap-plugin-capability__hint">{t("caps.pluginSkillsHint")}</div>
			<div className="cap-plugin-capability__list">
				{skills.map((skill) => (
					<div className="cap-plugin-capability__item" key={`${skill.name}-${skill.path || skill.invocation || ""}`}>
						<div className="cap-plugin-capability__line">
							<span className="cap-plugin-capability__name">{skill.invocation || `/${skill.name}`}</span>
							{skill.runAs && <span className="cap-source-badge">{skill.runAs}</span>}
						</div>
						<div className="cap-plugin-capability__desc">{skill.description || t("caps.pluginNoDescription")}</div>
					</div>
				))}
			</div>
		</div>
	);
}

function PluginHookList({ hooks }: { hooks: PluginHookView[] }) {
	const t = useT();
	return (
		<div className="cap-plugin-capability">
			<div className="cap-plugin-capability__head">{t("caps.pluginHooksTitle")}</div>
			<div className="cap-plugin-capability__hint">{t("caps.pluginHooksHint")}</div>
			<div className="cap-plugin-capability__list">
				{hooks.map((hook, idx) => {
					const target = hook.command || hook.contextFile || t("caps.pluginHookNoTarget");
					return (
						<div className="cap-plugin-capability__item" key={`${hook.event}-${hook.match || "*"}-${target}-${idx}`}>
							<div className="cap-plugin-capability__line">
								<span className="cap-plugin-capability__name">{hook.event}</span>
								<span className="cap-source-badge">{hook.match || "*"}</span>
							</div>
							<div className="cap-plugin-capability__desc">{hook.description || target}</div>
						</div>
					);
				})}
			</div>
		</div>
	);
}

function PluginMCPList({ servers }: { servers: PluginMCPServerView[] }) {
	const t = useT();
	return (
		<div className="cap-plugin-capability">
			<div className="cap-plugin-capability__head">{t("caps.pluginMCPTitle")}</div>
			<div className="cap-plugin-capability__hint">{t("caps.pluginMCPHint")}</div>
			<div className="cap-plugin-capability__list">
				{servers.map((server) => (
					<div className="cap-plugin-capability__item" key={server.name}>
						<div className="cap-plugin-capability__line">
							<span className="cap-plugin-capability__name">{server.displayName || server.name}</span>
							{server.transport && <span className="cap-source-badge">{server.transport}</span>}
							<span className="cap-source-badge">{server.autoStart ? t("caps.pluginMCPAutoStart") : t("caps.pluginMCPOnDemand")}</span>
						</div>
						<div className="cap-plugin-capability__desc">{server.description || server.command || server.url || t("caps.pluginMCPNoTarget")}</div>
					</div>
				))}
			</div>
		</div>
	);
}

function normalizePluginViews(plugins: PluginView[] | null | undefined): PluginView[] {
	return sortPluginsForDisplay(asArray(plugins).map(normalizePluginView));
}

function normalizePluginView(plugin: PluginView): PluginView {
	return {
		...plugin,
		name: plugin.name || "plugin",
		root: plugin.root || "",
		enabled: Boolean(plugin.enabled),
		skills: Number.isFinite(plugin.skills) ? plugin.skills : 0,
		commands: Number.isFinite(plugin.commands) ? plugin.commands : 0,
		agents: Number.isFinite(plugin.agents) ? plugin.agents : 0,
		hooks: Number.isFinite(plugin.hooks) ? plugin.hooks : 0,
		mcpServers: Number.isFinite(plugin.mcpServers) ? plugin.mcpServers : 0,
		skillDetails: asArray(plugin.skillDetails),
		agentDetails: asArray(plugin.agentDetails),
		commandDetails: asArray(plugin.commandDetails),
		hookDetails: asArray(plugin.hookDetails),
		mcpServerDetails: asArray(plugin.mcpServerDetails),
		warnings: asArray(plugin.warnings),
	};
}

function sortPluginsForDisplay(plugins: PluginView[]): PluginView[] {
	return [...plugins].sort((a, b) => {
		const priority = pluginDisplayPriority(a) - pluginDisplayPriority(b);
		if (priority !== 0) return priority;
		return a.name.localeCompare(b.name, undefined, { sensitivity: "base" });
	});
}

function pluginDisplayPriority(plugin: PluginView): number {
	if (plugin.error) return 0;
	if (plugin.enabled) return 1;
	return 2;
}

function pluginListSummary(plugins: PluginView[], t: ReturnType<typeof useT>): string {
	const enabled = plugins.filter((plugin) => plugin.enabled && !plugin.error).length;
	const issues = plugins.filter((plugin) => Boolean(plugin.error) || asArray(plugin.warnings).length > 0).length;
	return t("caps.pluginsSummary", { enabled, total: plugins.length, issues });
}

function pluginCapabilitiesSummary(plugin: PluginView, t: ReturnType<typeof useT>): string {
	if (plugin.skills === 0 && (plugin.agents || 0) === 0 && (plugin.commands || 0) === 0 && plugin.hooks === 0 && plugin.mcpServers === 0) return t("caps.pluginNoCapabilities");
	return t("caps.pluginCounts", { skills: plugin.skills, agents: plugin.agents || 0, commands: plugin.commands || 0, hooks: plugin.hooks, mcps: plugin.mcpServers });
}

function pluginCompatibilityLabel(status: string, t: ReturnType<typeof useT>): string {
	if (status === "full") return t("caps.pluginCompatibilityFull");
	if (status === "partial") return t("caps.pluginCompatibilityPartial");
	if (status === "none") return t("caps.pluginCompatibilityNone");
	return status;
}

function pluginWarnings(plugin: PluginView, diagnostic?: PluginView): string[] {
	const warnings = [...asArray(plugin.warnings), ...asArray(diagnostic?.warnings)];
	return Array.from(new Set(warnings.filter((warning) => warning.trim().length > 0)));
}

function parsePluginInstallPlan(raw: string): PluginInstallPlanView {
	try {
		const value = JSON.parse(raw) as Record<string, unknown>;
		const actions = (Array.isArray(value.actions) ? value.actions : []).flatMap((action) => {
			if (!action || typeof action !== "object") return [];
			const item = action as Record<string, unknown>;
			return [{
				action: stringValue(item.action),
				kind: stringValue(item.kind),
				name: stringValue(item.name),
				source: stringValue(item.source),
				status: stringValue(item.status),
				message: stringValue(item.message),
				error: stringValue(item.error),
				compatibility: stringValue(item.compatibility),
				mappedCapabilities: (Array.isArray(item.mappedCapabilities) ? item.mappedCapabilities : []).filter((value): value is string => typeof value === "string"),
				skippedCapabilities: (Array.isArray(item.skippedCapabilities) ? item.skippedCapabilities : []) as PluginCompatibilityIssue[],
				agentCount: numericValue(item.agentCount), skillCount: numericValue(item.skillCount), commandCount: numericValue(item.commandCount), hookCount: numericValue(item.hookCount), toolCount: numericValue(item.toolCount),
			}];
		});
		return {
			raw,
			ok: typeof value.ok === "boolean" ? value.ok : undefined,
			status: stringValue(value.status),
			name: stringValue(value.name),
			actions,
			warnings: (Array.isArray(value.warnings) ? value.warnings : []).flatMap((warning) => typeof warning === "string" ? [warning] : []),
			error: stringValue(value.error),
		};
	} catch {
		return { raw, actions: [], warnings: [] };
	}
}

function numericValue(value: unknown): number | undefined {
	return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function stringValue(value: unknown): string | undefined {
	return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

function pluginPlanActionLabel(action: PluginInstallPlanAction, t: ReturnType<typeof useT>): string {
	const verb = action.action || action.kind || t("caps.pluginAction");
	return [verb, action.name].filter(Boolean).join(" · ");
}

function pluginPlanNotice(plan: PluginInstallPlanView, t: ReturnType<typeof useT>): string {
	if (plan.error) return plan.error;
	if (plan.status === "done" || plan.status === "applied" || plan.status === "complete") return t("caps.pluginPlanInstalled");
	return plan.status ? t("caps.pluginPlanStatus", { status: plan.status }) : t("caps.pluginPlanComplete");
}

type MCPSettingsScreen =
	| { kind: "list" }
	| { kind: "add" }
	| { kind: "marketplace" }
	| { kind: "detail"; name: string }
	| { kind: "edit"; name: string };

type MCPServerEditorDraft = {
	name: string;
	transport: string;
	command: string;
	structuredCommand?: {
		display: string;
		command: string;
		args: string[];
	};
	url: string;
	env: string;
	headers: string;
	autoStart?: boolean;
	callTimeoutSeconds?: number;
	toolTimeoutSeconds?: Record<string, number>;
};

type MCPServerJSONError = "invalid" | "single" | "name" | "required" | "unsupported";

function mcpServerSchemaIssueCount(server: ServerView): number {
	return (server.toolList ?? []).filter((tool) => tool.schemaError).length;
}

function mcpSettingsServerSummary(server: ServerView, t: ReturnType<typeof useT>): string {
	if (server.status === "failed") {
		return server.authStatus === "required" ? t("caps.authRequiredSummary") : summarizeServerError(server.error || t("caps.failed"));
	}
	if (server.status !== "connected") return serverStatusLabel(server, t);
	const unavailable = mcpServerSchemaIssueCount(server);
	const parts = [serverStatusLabel(server, t), t("caps.serverToolSummary", { tools: server.tools || 0 })];
	if (unavailable > 0) parts.push(t("caps.schemaIssues", { count: unavailable }));
	return parts.join(" · ");
}

function mcpServerSourceLabel(server: ServerView, t: ReturnType<typeof useT>): string {
	switch (server.source) {
		case "project":
			return server.configSource
				? t("caps.sourceProjectConfig", { config: server.configSource })
				: t("caps.sourceProject");
		case "plugin":
			return t("caps.sourcePlugin");
		case "builtin":
			return t("caps.sourceBuiltin");
		default:
			return t("caps.sourceUser");
	}
}

function mcpSettingsSearchText(server: ServerView): string {
	return [
		server.name,
		server.transport,
		serverCommand(server),
		server.error,
		server.source,
		server.configSource,
		server.managedByPlugin,
		...(server.toolList ?? []).flatMap((tool) => [tool.name, tool.description]),
	].filter(Boolean).join(" ").toLowerCase();
}

function MCPSettingsSubpageHeader({
	title,
	description,
	onBack,
}: {
	title: string;
	description: string;
	onBack: () => void;
}) {
	const t = useT();
	return (
		<header className="cap-mcp-subpage__header">
			<button className="cap-mcp-subpage__back" type="button" onClick={onBack}>
				<ArrowLeft aria-hidden size={14} />
				{t("caps.backToServers")}
			</button>
			<h3 className="cap-mcp-subpage__title">{title}</h3>
			<p className="cap-mcp-subpage__desc">{description}</p>
		</header>
	);
}

function MCPSettingsServerRow({
	server,
	busy,
	onOpen,
	onRetry,
	onToggle,
}: {
	server: ServerView;
	busy: boolean;
	onOpen: () => void;
	onRetry: () => void;
	onToggle: (enabled: boolean) => void;
}) {
	const t = useT();
	const lifecycle = mcpServerLifecycleActions(server);
	const target = serverCommand(server);
	const opensAuth = shouldOpenAuth(server);
	const actionLabel = serverActionLabel(server, t);
	const handlePrimaryAction = () => {
		if (opensAuth) {
			openExternal((server.authUrl || "").trim());
			return;
		}
		onRetry();
	};

	return (
		<div className={`cap-mcp-list-row${server.status === "disabled" ? " cap-mcp-list-row--disabled" : ""}`} data-status={server.status}>
			<button className="cap-mcp-list-row__main" type="button" onClick={onOpen}>
				<span className="cap-mcp-list-row__icon" aria-hidden>
					<ServerIcon size={16} strokeWidth={1.8} />
				</span>
				<span className="cap-mcp-list-row__copy">
					<span className="cap-mcp-list-row__head">
						<span className={`cap-dot cap-dot--${server.status}`} aria-hidden />
						<span className="cap-mcp-list-row__name">{server.name}</span>
						<span className="cap-mcp-list-row__transport">{server.transport}</span>
						{server.source === "project" && <span className="cap-row__builtin">{t("caps.projectServerBadge")}</span>}
						{server.builtIn && <span className="cap-row__builtin">{t("caps.builtIn")}</span>}
					</span>
					<span className={`cap-mcp-list-row__summary${server.status === "failed" ? " cap-mcp-list-row__summary--error" : ""}`}>
						{mcpSettingsServerSummary(server, t)}
					</span>
					{target && <span className="cap-mcp-list-row__target">{target}</span>}
					{server.managedByPlugin && (
						<span className="cap-mcp-list-row__owner">{t("caps.managedByPlugin", { plugin: server.managedByPlugin })}</span>
					)}
				</span>
				<ChevronRight className="cap-mcp-list-row__chevron" aria-hidden size={16} />
			</button>
			<div className="cap-mcp-list-row__actions">
				{lifecycle.showRetryInRow ? (
					<button className="btn btn--small" disabled={busy} type="button" onClick={handlePrimaryAction}>
						{actionLabel}
					</button>
				) : !server.managedByPlugin ? (
					<Tooltip label={lifecycle.enabled ? t("caps.disable") : t("caps.enable")}>
						<label className="cap-switch">
							<input
								type="checkbox"
								checked={lifecycle.enabled}
								disabled={busy}
								onChange={(event) => onToggle(event.target.checked)}
							/>
							<span className="cap-switch__track" />
						</label>
					</Tooltip>
				) : null}
			</div>
		</div>
	);
}

function MCPSettingsServerGroup({
	title,
	hint,
	servers,
	busy,
	onOpen,
	onRetry,
	onToggle,
}: {
	title: string;
	hint?: string;
	servers: ServerView[];
	busy: boolean;
	onOpen: (name: string) => void;
	onRetry: (name: string) => void;
	onToggle: (name: string, enabled: boolean) => void;
}) {
	if (servers.length === 0) return null;
	return (
		<section className="cap-mcp-list-section">
			<div className="cap-mcp-list-section__head">
				<div>
					<div className="cap-mcp-list-section__title">{title} <span>{servers.length}</span></div>
					{hint && <div className="cap-mcp-list-section__hint">{hint}</div>}
				</div>
			</div>
			<div className="cap-mcp-list">
				{servers.map((server) => (
					<MCPSettingsServerRow
						key={server.name}
						server={server}
						busy={busy}
						onOpen={() => onOpen(server.name)}
						onRetry={() => onRetry(server.name)}
						onToggle={(enabled) => onToggle(server.name, enabled)}
					/>
				))}
			</div>
		</section>
	);
}

function mcpServerEditorDraft(server?: ServerView): MCPServerEditorDraft {
	const transport = normalizeTransportValue(server?.transport || "stdio");
	const command = server && transport === "stdio" ? serverCommand(server) : "";
	return {
		name: server?.name || "",
		transport,
		command,
		structuredCommand: server && transport === "stdio" ? {
			display: command,
			command: server.command || "",
			args: [...(server.args ?? [])],
		} : undefined,
		url: server && transport !== "stdio" ? server.url || serverCommand(server) : "",
		env: "",
		headers: "",
		autoStart: server?.autoStart,
		callTimeoutSeconds: server?.callTimeoutSeconds,
		toolTimeoutSeconds: server?.toolTimeoutSeconds ? { ...server.toolTimeoutSeconds } : undefined,
	};
}

function mcpServerInputDraft(input: MCPServerInput): MCPServerEditorDraft {
	const transport = normalizeTransportValue(input.transport);
	const command = transport === "stdio" ? [input.command, ...input.args].filter(Boolean).join(" ").trim() : "";
	return {
		name: input.name,
		transport,
		command,
		structuredCommand: transport === "stdio" ? {
			display: command,
			command: input.command,
			args: [...input.args],
		} : undefined,
		url: transport === "stdio" ? "" : input.url,
		env: input.env ? Object.entries(input.env).map(([key, value]) => `${key}=${value}`).join("\n") : "",
		headers: input.headers ? Object.entries(input.headers).map(([key, value]) => `${key}=${value}`).join("\n") : "",
		autoStart: input.autoStart ?? undefined,
		callTimeoutSeconds: input.callTimeoutSeconds ?? undefined,
		toolTimeoutSeconds: input.toolTimeoutSeconds ? { ...input.toolTimeoutSeconds } : undefined,
	};
}

function mcpServerDraftInput(draft: MCPServerEditorDraft): MCPServerInput {
	const isStdio = draft.transport === "stdio";
	const structuredCommand = draft.structuredCommand?.display === draft.command ? draft.structuredCommand : undefined;
	const envText = draft.env.trim();
	const headerText = draft.headers.trim();
	return {
		name: draft.name.trim(),
		transport: draft.transport,
		command: isStdio ? structuredCommand?.command || draft.command.trim() : "",
		args: isStdio ? structuredCommand?.args ?? [] : [],
		url: isStdio ? "" : draft.url.trim(),
		env: envText ? parseKeyValueText(envText) : null,
		headers: !isStdio && headerText ? parseKeyValueText(headerText) : null,
		autoStart: draft.autoStart ?? null,
		callTimeoutSeconds: draft.callTimeoutSeconds ?? null,
		toolTimeoutSeconds: draft.toolTimeoutSeconds ?? null,
	};
}

function mcpMarketplaceServerInput(entry: MCPMarketplaceEntry, servers: ServerView[]): MCPServerInput {
	const used = new Set(servers.map((server) => server.name));
	const base = entry.suggestedName || entry.name.split("/").filter(Boolean).pop() || "mcp-server";
	let name = base;
	for (let suffix = 2; used.has(name); suffix += 1) name = `${base}-${suffix}`;
	const transport = entry.transport || "stdio";
	return {
		name,
		transport,
		command: transport === "stdio" ? entry.command || "" : "",
		args: transport === "stdio" ? [...(entry.args ?? [])] : [],
		url: transport === "stdio" ? "" : entry.url || "",
		env: null,
		headers: null,
		autoStart: null,
		callTimeoutSeconds: null,
		toolTimeoutSeconds: null,
	};
}

export function mcpServerDraftJSON(draft: MCPServerEditorDraft): string {
	const input = mcpServerDraftInput(draft);
	const entry: Record<string, unknown> = { type: input.transport };
	if (input.transport === "stdio") {
		entry.command = input.command;
		if (input.args.length > 0) entry.args = input.args;
	}
	else entry.url = input.url;
	if (input.env && Object.keys(input.env).length > 0) entry.env = input.env;
	if (input.headers && Object.keys(input.headers).length > 0) entry.headers = input.headers;
	if (input.autoStart != null) entry.auto_start = input.autoStart;
	if (input.callTimeoutSeconds != null) entry.call_timeout_seconds = input.callTimeoutSeconds;
	if (input.toolTimeoutSeconds && Object.keys(input.toolTimeoutSeconds).length > 0) entry.tool_timeout_seconds = input.toolTimeoutSeconds;
	return JSON.stringify({ [input.name || "server-name"]: entry }, null, 2);
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function stringRecord(value: unknown): Record<string, string> | null {
	if (value == null) return null;
	if (!isRecord(value) || Object.values(value).some((item) => typeof item !== "string")) throw new Error("invalid");
	return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, item as string]));
}

function assertSupportedKeys(value: Record<string, unknown>, supported: readonly string[]) {
	const allowed = new Set(supported);
	if (Object.keys(value).some((key) => !allowed.has(key))) throw new Error("unsupported" satisfies MCPServerJSONError);
}

function nonNegativeInteger(value: unknown): number | undefined {
	if (value == null) return undefined;
	if (typeof value !== "number" || !Number.isInteger(value) || value < 0) throw new Error("invalid" satisfies MCPServerJSONError);
	return value;
}

function nonNegativeIntegerRecord(value: unknown): Record<string, number> | undefined {
	if (value == null) return undefined;
	if (!isRecord(value)) throw new Error("invalid" satisfies MCPServerJSONError);
	const out: Record<string, number> = {};
	for (const [name, item] of Object.entries(value)) {
		if (!name.trim()) throw new Error("invalid" satisfies MCPServerJSONError);
		const seconds = nonNegativeInteger(item);
		if (seconds === undefined) throw new Error("invalid" satisfies MCPServerJSONError);
		out[name] = seconds;
	}
	return out;
}

// withExplicitMCPClears finalizes an edit of an existing server. The editor
// seeds every non-secret setting into the draft/JSON, so a field the user
// removed must clear the persisted value instead of being preserved as
// "absent". env/headers stay preserve-on-absent because their values are
// deliberately never seeded into the editor.
export function withExplicitMCPClears(input: MCPServerInput): MCPServerInput {
	return {
		...input,
		autoStart: input.autoStart ?? true,
		callTimeoutSeconds: input.callTimeoutSeconds ?? 0,
		toolTimeoutSeconds: input.toolTimeoutSeconds ?? {},
	};
}

export function parseMCPServerJSON(raw: string, fixedName?: string, options?: { allowIncomplete?: boolean }): { input: MCPServerInput; draft: MCPServerEditorDraft } {
	let parsed: unknown;
	try {
		parsed = JSON.parse(raw);
	} catch {
		throw new Error("invalid" satisfies MCPServerJSONError);
	}
	if (!isRecord(parsed)) throw new Error("single" satisfies MCPServerJSONError);
	if (isRecord(parsed.mcpServers)) assertSupportedKeys(parsed, ["mcpServers"]);
	const container = isRecord(parsed.mcpServers) ? parsed.mcpServers : parsed;
	const entries = Object.entries(container);
	if (entries.length !== 1) throw new Error("single" satisfies MCPServerJSONError);
	const [name, value] = entries[0];
	if (!name.trim() || !isRecord(value)) throw new Error("single" satisfies MCPServerJSONError);
	assertSupportedKeys(value, [
		"type", "transport", "command", "args", "url", "env", "headers", "auto_start",
		"call_timeout_seconds", "tool_timeout_seconds", "trusted_read_only_tools",
		"default_tools_approval_mode", "tools", "approvals_reviewer",
	]);
	if (fixedName && name !== fixedName) throw new Error("name" satisfies MCPServerJSONError);
	if (value.type != null && typeof value.type !== "string") throw new Error("invalid" satisfies MCPServerJSONError);
	if (value.transport != null && typeof value.transport !== "string") throw new Error("invalid" satisfies MCPServerJSONError);
	if (value.type != null && value.transport != null) throw new Error("unsupported" satisfies MCPServerJSONError);
	const transportValue = typeof value.type === "string" ? value.type : value.transport;
	const transport = normalizeTransportValue(typeof transportValue === "string" ? transportValue : (typeof value.url === "string" ? "http" : "stdio"));
	if (transport !== "stdio" && transport !== "http" && transport !== "sse") throw new Error("invalid" satisfies MCPServerJSONError);
	if (transport === "stdio" && (value.url != null || value.headers != null)) throw new Error("unsupported" satisfies MCPServerJSONError);
	if (transport !== "stdio" && (value.command != null || value.args != null)) throw new Error("unsupported" satisfies MCPServerJSONError);
	const command = typeof value.command === "string" ? value.command.trim() : "";
	if (value.args != null && (!Array.isArray(value.args) || !value.args.every((arg) => typeof arg === "string"))) throw new Error("invalid" satisfies MCPServerJSONError);
	const args = value.args ? value.args as string[] : [];
	const url = typeof value.url === "string" ? value.url.trim() : "";
	if (!options?.allowIncomplete && ((transport === "stdio" && !command) || (transport !== "stdio" && !url))) {
		throw new Error("required" satisfies MCPServerJSONError);
	}
	let env: Record<string, string> | null;
	let headers: Record<string, string> | null;
	try {
		env = stringRecord(value.env);
		headers = stringRecord(value.headers);
	} catch {
		throw new Error("invalid" satisfies MCPServerJSONError);
	}
	if (value.auto_start != null && typeof value.auto_start !== "boolean") throw new Error("invalid" satisfies MCPServerJSONError);
	const autoStart = value.auto_start as boolean | undefined;
	const callTimeoutSeconds = nonNegativeInteger(value.call_timeout_seconds);
	const toolTimeoutSeconds = nonNegativeIntegerRecord(value.tool_timeout_seconds);
	const input: MCPServerInput = {
		name: fixedName || name,
		transport,
		command: transport === "stdio" ? command : "",
		args: transport === "stdio" ? args : [],
		url: transport === "stdio" ? "" : url,
		env,
		headers: transport === "stdio" ? null : headers,
		autoStart: autoStart ?? null,
		callTimeoutSeconds: callTimeoutSeconds ?? null,
		toolTimeoutSeconds: toolTimeoutSeconds ?? null,
	};
	return {
		input,
			draft: {
				name: input.name,
				transport,
				command: [command, ...args].filter(Boolean).join(" "),
				structuredCommand: transport === "stdio" ? {
					display: [command, ...args].filter(Boolean).join(" "),
					command,
					args: [...args],
				} : undefined,
			url,
			env: env ? Object.entries(env).map(([key, item]) => `${key}=${item}`).join("\n") : "",
			headers: headers ? Object.entries(headers).map(([key, item]) => `${key}=${item}`).join("\n") : "",
			autoStart,
			callTimeoutSeconds,
			toolTimeoutSeconds,
		},
	};
}

function mcpServerJSONErrorLabel(error: unknown, t: ReturnType<typeof useT>): string {
	const code = error instanceof Error ? error.message as MCPServerJSONError : "invalid";
	if (code === "single") return t("caps.jsonSingleServer");
	if (code === "name") return t("caps.jsonNameMismatch");
	if (code === "required") return t("caps.jsonRequired");
	if (code === "unsupported") return t("caps.jsonUnsupported");
	return t("caps.jsonInvalid");
}

function MCPServerSettingsEditor({
	server,
	busy,
	onCancel,
	onSubmit,
}: {
	server?: ServerView;
	busy: boolean;
	onCancel: () => void;
	onSubmit: (input: MCPServerInput) => void;
}) {
	const t = useT();
	type EditorMode = "quick" | "form" | "json";
	const [mode, setMode] = useState<EditorMode>(server ? "form" : "quick");
	const [definition, setDefinition] = useState("");
	const [quickError, setQuickError] = useState("");
	const [draft, setDraft] = useState<MCPServerEditorDraft>(() => mcpServerEditorDraft(server));
	const [json, setJSON] = useState(() => mcpServerDraftJSON(mcpServerEditorDraft(server)));
	const [jsonError, setJSONError] = useState("");
	const [advancedOpen, setAdvancedOpen] = useState(false);
	const isStdio = draft.transport === "stdio";
	const ready = Boolean(draft.name.trim() && (isStdio ? draft.command.trim() : draft.url.trim()));

	const updateDraft = (patch: Partial<MCPServerEditorDraft>) => setDraft((current) => ({ ...current, ...patch }));
	const switchMode = (next: EditorMode) => {
		if (next === mode) return;
		if (next === "quick") {
			setQuickError("");
			setMode("quick");
			return;
		}
		if (mode === "quick") {
			if (definition.trim()) {
				try {
					const nextDraft = mcpServerInputDraft(parseMCPQuickDefinition(definition));
					setDraft(nextDraft);
					if (next === "json") setJSON(mcpServerDraftJSON(nextDraft));
					setQuickError("");
				} catch (error) {
					setQuickError(mcpServerJSONErrorLabel(error, t));
					return;
				}
			}
			setMode(next);
			return;
		}
		if (next === "json") {
			setJSON(mcpServerDraftJSON(draft));
			setJSONError("");
			setMode("json");
			return;
		}
		if (json === mcpServerDraftJSON(draft)) {
			setJSONError("");
			setMode("form");
			return;
		}
		try {
			const parsed = parseMCPServerJSON(json, server?.name, { allowIncomplete: true });
			setDraft(parsed.draft);
			setJSONError("");
			setMode("form");
		} catch (error) {
			setJSONError(mcpServerJSONErrorLabel(error, t));
		}
	};
	const finalize = (input: MCPServerInput) => (server ? withExplicitMCPClears(input) : input);
	const submit = () => {
		if (mode === "quick") {
			try {
				setQuickError("");
				onSubmit(parseMCPQuickDefinition(definition));
			} catch (error) {
				setQuickError(mcpServerJSONErrorLabel(error, t));
			}
			return;
		}
		if (mode === "form") {
			onSubmit(finalize(mcpServerDraftInput(draft)));
			return;
		}
		try {
			const parsed = parseMCPServerJSON(json, server?.name);
			setJSONError("");
			onSubmit(finalize(parsed.input));
		} catch (error) {
			setJSONError(mcpServerJSONErrorLabel(error, t));
		}
	};

	return (
		<div className="cap-mcp-editor">
			<div className="cap-mcp-editor__mode set-seg" role="tablist" aria-label={t("caps.editorMode")}>
				{!server && (
					<button className={`set-seg__btn${mode === "quick" ? " set-seg__btn--on" : ""}`} type="button" role="tab" aria-selected={mode === "quick"} onClick={() => switchMode("quick")}>
						{t("caps.quickMode")}
					</button>
				)}
				<button className={`set-seg__btn${mode === "form" ? " set-seg__btn--on" : ""}`} type="button" role="tab" aria-selected={mode === "form"} onClick={() => switchMode("form")}>
					{t("caps.formMode")}
				</button>
				<button className={`set-seg__btn${mode === "json" ? " set-seg__btn--on" : ""}`} type="button" role="tab" aria-selected={mode === "json"} onClick={() => switchMode("json")}>
					{t("caps.jsonMode")}
				</button>
			</div>
			{mode === "quick" ? (
				<div className="cap-mcp-quick">
					<label className="cap-mcp-field">
						<span>{t("caps.installDefinition")}</span>
						<textarea
							className="mem-textarea cap-mcp-quick__input"
							value={definition}
							disabled={busy}
							onChange={(event) => { setDefinition(event.target.value); setQuickError(""); }}
							placeholder={t("caps.installDefinitionPlaceholder")}
							spellCheck={false}
						/>
					</label>
					<div className="cap-mcp-quick__hint">{t("caps.installDefinitionHint")}</div>
					<div className="cap-mcp-quick__benefits" aria-label={t("caps.quickBenefitsLabel")}>
						<span>{t("caps.quickDetectTransport")}</span>
						<span>{t("caps.quickVerifyConnection")}</span>
						<span>{t("caps.quickEnableTools")}</span>
					</div>
					{quickError && <div className="banner banner--error" role="alert">{quickError}</div>}
				</div>
			) : mode === "form" ? (
				<div className="cap-mcp-form-grid">
					<label className="cap-mcp-field cap-mcp-field--name">
						<span>{t("caps.name")}</span>
						<input className="mem-input" value={draft.name} disabled={busy || Boolean(server)} onChange={(event) => updateDraft({ name: event.target.value })} placeholder={t("caps.namePlaceholder")} />
					</label>
					<label className="cap-mcp-field cap-mcp-field--transport">
						<span>{t("caps.transport")}</span>
						<select className="mem-select" value={draft.transport} disabled={busy} onChange={(event) => updateDraft({ transport: normalizeTransportValue(event.target.value) })}>
							<option value="stdio">stdio</option>
							<option value="http">http</option>
							<option value="sse">sse</option>
						</select>
					</label>
					{isStdio ? (
						<label className="cap-mcp-field cap-mcp-field--wide">
							<span>{t("caps.command")}</span>
							<input className="mem-input" value={draft.command} disabled={busy} onChange={(event) => updateDraft({ command: event.target.value })} placeholder={t("caps.commandPlaceholder")} />
						</label>
					) : (
						<label className="cap-mcp-field cap-mcp-field--wide">
							<span>{t("caps.url")}</span>
							<input className="mem-input" value={draft.url} disabled={busy} onChange={(event) => updateDraft({ url: event.target.value })} placeholder={t("caps.urlPlaceholder")} />
						</label>
					)}
					<div className="cap-mcp-advanced cap-mcp-field--wide">
						<button className="cap-mcp-advanced__toggle" type="button" aria-expanded={advancedOpen} onClick={() => setAdvancedOpen((open) => !open)}>
							{advancedOpen ? <ChevronDown aria-hidden size={14} /> : <ChevronRight aria-hidden size={14} />}
							{advancedOpen ? t("caps.hideAdvancedOptions") : t("caps.advancedOptions")}
						</button>
						{advancedOpen && (
							<div className="cap-mcp-advanced__body">
								{!isStdio && (
									<label className="cap-mcp-field">
										<span>{t("caps.headersLabel")}</span>
										<textarea className="mem-textarea" value={draft.headers} disabled={busy} onChange={(event) => updateDraft({ headers: event.target.value })} placeholder={t("caps.headersPlaceholder")} spellCheck={false} />
										{server?.headerKeys && server.headerKeys.length > 0 && <small>{t("caps.headersPreserveHint")}</small>}
									</label>
								)}
								<label className="cap-mcp-field">
									<span>{t("caps.envLabel")}</span>
									<textarea className="mem-textarea" value={draft.env} disabled={busy} onChange={(event) => updateDraft({ env: event.target.value })} placeholder={t("caps.envPlaceholder")} spellCheck={false} />
									{server?.envKeys && server.envKeys.length > 0 && <small>{t("caps.envPreserveHint")}</small>}
								</label>
							</div>
						)}
					</div>
				</div>
			) : (
				<div className="cap-mcp-json-editor">
					<label className="cap-mcp-field">
						<span>{t("caps.jsonConfig")}</span>
						<textarea className="mem-textarea cap-mcp-json-editor__input" value={json} disabled={busy} onInput={(event) => { setJSON(event.currentTarget.value); setJSONError(""); }} spellCheck={false} />
					</label>
					<div className="cap-mcp-json-editor__hint">{t("caps.jsonPasteHint")}</div>
					{jsonError && <div className="banner banner--error" role="alert">{jsonError}</div>}
				</div>
			)}
			<div className="cap-mcp-editor__actions">
				<button className="btn btn--small" disabled={busy} type="button" onClick={onCancel}>{t("common.cancel")}</button>
				<button className="btn btn--primary btn--small" disabled={busy || (mode === "quick" ? !definition.trim() : mode === "form" && !ready)} type="button" onClick={submit}>
					{server ? t("caps.saveConfig") : t("caps.addAndConnect")}
				</button>
			</div>
		</div>
	);
}

// MCPServersSettingsPage is a self-contained MCP servers management page
// embedded inside the settings centre.
export function MCPServersSettingsPage() {
	const t = useT();
	const [snapshotKey, setSnapshotKey] = useState("");
	const [servers, setServers] = useState<ServerView[] | null>(null);
	const [busy, setBusy] = useState(false);
	const [err, setErr] = useState<string | null>(null);
	const [query, setQuery] = useState("");
	const [screen, setScreen] = useState<MCPSettingsScreen>({ kind: "list" });
	const [marketplace, setMarketplace] = useState<MCPMarketplaceView | null>(null);
	const [marketplaceQuery, setMarketplaceQuery] = useState("");

	const reload = useCallback(async () => {
		const [meta, tabs] = await Promise.all([
			app.Meta().catch(() => null),
			app.ListTabs().catch(() => []),
		]);
		const key = settingsSnapshotKey(meta, tabs);
		setSnapshotKey(key);
		const cached = key ? mcpSettingsSnapshot : null;
		if (cached?.key === key) {
			setServers(cached.value);
		} else {
			setServers(null);
		}
		const next = normalizeServerViews(await app.MCPServers().catch(() => []));
		mcpSettingsSnapshot = { key, value: next };
		setServers(next);
	}, []);
	useEffect(() => { void reload(); }, [reload]);
	useEffect(() => {
		if (!servers?.some((s) => s.status === "initializing" || s.status === "deferred")) return;
		const id = window.setInterval(() => void reload(), 2500);
		return () => window.clearInterval(id);
	}, [reload, servers]);

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
	const browseMarketplace = async (search = marketplaceQuery) => {
		setBusy(true);
		setErr(null);
		try {
			const result = await app.MCPMarketplace(search);
			setMarketplace({ ...result, servers: asArray(result.servers) });
			return true;
		} catch (error) {
			setErr(String((error as Error)?.message ?? error));
			return false;
		} finally {
			setBusy(false);
		}
	};
	const openMarketplace = () => {
		setScreen({ kind: "marketplace" });
		if (marketplace === null) void browseMarketplace("");
	};
	const installMarketplaceEntry = async (entry: MCPMarketplaceEntry) => {
		const current = await app.MCPMarketplaceResolve(entry.name);
		return installMCPServer(mcpMarketplaceServerInput(current, servers ?? []));
	};
	const filteredServers = useMemo(() => {
		const sorted = sortServersForDisplay(servers ?? []);
		const normalizedQuery = query.trim().toLowerCase();
		return normalizedQuery ? sorted.filter((server) => mcpSettingsSearchText(server).includes(normalizedQuery)) : sorted;
	}, [query, servers]);
	const projectServers = useMemo(() => filteredServers.filter((server) => server.source === "project"), [filteredServers]);
	const managedServers = useMemo(
		() => filteredServers.filter((server) => server.source === "plugin" || Boolean(server.managedByPlugin)),
		[filteredServers],
	);
	const installedServers = useMemo(
		() => filteredServers.filter((server) => server.source !== "project" && server.source !== "plugin" && !server.managedByPlugin),
		[filteredServers],
	);
	const selectedServer = screen.kind === "detail" || screen.kind === "edit"
		? servers?.find((server) => server.name === screen.name)
		: undefined;
	useEffect(() => {
		if (servers && (screen.kind === "detail" || screen.kind === "edit") && !servers.some((server) => server.name === screen.name)) {
			setScreen({ kind: "list" });
		}
	}, [screen, servers]);

	const summary = useMemo(() => {
		if (!servers) return "";
		return mcpServerSummary(servers, t);
	}, [servers, t]);

	const loading = servers === null;
	const actionBusy = busy || !snapshotKey || loading;

	return (
		<section className="cap-mcp-settings">
			{err && <div className="banner banner--error" role="alert">{err}</div>}
			{screen.kind === "list" && (
				<>
					<div className="cap-mcp-list-toolbar">
						{servers && servers.length > 0 ? <div className="drawer__summary">{summary}</div> : <span />}
						<div className="cap-mcp-list-toolbar__actions">
							<Tooltip label={t("caps.refresh")}>
								<button className="cap-mcp-icon-btn" type="button" aria-label={t("caps.refresh")} disabled={actionBusy} onClick={() => void reload()}>
									<RefreshCw aria-hidden size={15} />
								</button>
							</Tooltip>
							<button className="btn btn--small" disabled={actionBusy} type="button" onClick={openMarketplace}>
								<Search aria-hidden size={14} />
								{t("caps.browseRegistry")}
							</button>
							<button className="btn btn--primary btn--small cap-mcp-add-btn" disabled={actionBusy} type="button" onClick={() => setScreen({ kind: "add" })}>
								<Plus aria-hidden size={14} />
								{t("caps.addServer")}
							</button>
						</div>
					</div>
					<label className="cap-mcp-search">
						<Search aria-hidden size={15} />
						<input type="search" value={query} onInput={(event) => setQuery(event.currentTarget.value)} placeholder={t("caps.searchServers")} />
					</label>
					{loading && <div className="mem-empty">{t("caps.loading")}</div>}
					{!loading && servers.length === 0 && <div className="mem-empty">{t("caps.noServers")}</div>}
					{!loading && servers.length > 0 && filteredServers.length === 0 && <div className="mem-empty">{t("caps.noServerMatches")}</div>}
					<MCPSettingsServerGroup
						title={t("caps.projectServers")}
						hint={t("caps.projectServersHint")}
						servers={projectServers}
						busy={actionBusy}
						onOpen={(name) => setScreen({ kind: "detail", name })}
						onRetry={(name) => void mutate(() => app.ReconnectMCPServer(name))}
						onToggle={(name, enabled) => void mutate(() => app.SetMCPServerEnabled(name, enabled))}
					/>
					<MCPSettingsServerGroup
						title={t("caps.installedServers")}
						hint={t("caps.installedServersHint")}
						servers={installedServers}
						busy={actionBusy}
						onOpen={(name) => setScreen({ kind: "detail", name })}
						onRetry={(name) => void mutate(() => app.ReconnectMCPServer(name))}
						onToggle={(name, enabled) => void mutate(() => app.SetMCPServerEnabled(name, enabled))}
					/>
					<MCPSettingsServerGroup
						title={t("caps.pluginServers")}
						hint={t("caps.pluginServersHint")}
						servers={managedServers}
						busy={actionBusy}
						onOpen={(name) => setScreen({ kind: "detail", name })}
						onRetry={(name) => void mutate(() => app.ReconnectMCPServer(name))}
						onToggle={(name, enabled) => void mutate(() => app.SetMCPServerEnabled(name, enabled))}
					/>
				</>
			)}
			{screen.kind === "marketplace" && (
				<div className="cap-mcp-subpage">
					<MCPSettingsSubpageHeader title={t("caps.registryTitle")} description={t("caps.registryHint")} onBack={() => setScreen({ kind: "list" })} />
					<form className="cap-mcp-search cap-mcp-search--action" onSubmit={(event) => { event.preventDefault(); void browseMarketplace(); }}>
						<Search aria-hidden size={15} />
						<input type="search" value={marketplaceQuery} onInput={(event) => setMarketplaceQuery(event.currentTarget.value)} placeholder={t("caps.searchRegistry")} />
						<button className="btn btn--small" disabled={busy} type="submit">{t("caps.search")}</button>
					</form>
					{marketplace?.warning && <div className="banner" role="status">{t("caps.registryCached")} {marketplace.warning}</div>}
					{busy && marketplace === null && <div className="mem-empty">{t("caps.loading")}</div>}
					{!busy && marketplace && marketplace.servers.length === 0 && <div className="mem-empty">{t("caps.noRegistryMatches")}</div>}
					{marketplace && marketplace.servers.length > 0 && (
						<div className="cap-mcp-list">
							{marketplace.servers.map((entry) => (
								<div className="cap-mcp-list-row" key={entry.name}>
									<div className="cap-mcp-list-row__main">
										<span className="cap-mcp-list-row__icon" aria-hidden><ServerIcon size={16} strokeWidth={1.8} /></span>
										<span className="cap-mcp-list-row__copy">
											<span className="cap-mcp-list-row__head">
												<span className="cap-mcp-list-row__name">{entry.title || entry.name}</span>
												{entry.version && <span className="cap-mcp-list-row__transport">{entry.version}</span>}
												{entry.transport && <span className="cap-mcp-list-row__transport">{entry.transport}</span>}
											</span>
											<span className="cap-mcp-list-row__target">{entry.name}</span>
											<span className="cap-mcp-list-row__summary">{entry.description || entry.unavailableReason}</span>
											{!entry.installable && entry.unavailableReason && <span className="cap-mcp-list-row__owner">{entry.unavailableReason}</span>}
										</span>
									</div>
									<div className="cap-mcp-list-row__actions">
										{entry.installable ? (
											<button className="btn btn--primary btn--small" disabled={actionBusy || marketplace.cached} type="button" onClick={() => void mutate(() => installMarketplaceEntry(entry)).then((ok) => { if (ok) setScreen({ kind: "list" }); })}>
												{t("caps.install")}
											</button>
										) : <span className="cap-mcp-list-row__owner">{t("caps.manualSetup")}</span>}
									</div>
								</div>
							))}
						</div>
					)}
				</div>
			)}
			{screen.kind === "add" && (
				<div className="cap-mcp-subpage">
					<MCPSettingsSubpageHeader title={t("caps.addServerTitle")} description={t("caps.addServerHint")} onBack={() => setScreen({ kind: "list" })} />
					<MCPServerSettingsEditor
						busy={busy}
						onCancel={() => setScreen({ kind: "list" })}
						onSubmit={(input) => void mutate(() => installMCPServer(input)).then((ok) => { if (ok) setScreen({ kind: "list" }); })}
					/>
				</div>
			)}
			{screen.kind === "edit" && selectedServer && (
				<div className="cap-mcp-subpage">
					<MCPSettingsSubpageHeader title={t("caps.editServerTitle", { name: selectedServer.name })} description={t("caps.editServerHint")} onBack={() => setScreen({ kind: "detail", name: selectedServer.name })} />
					<MCPServerSettingsEditor
						server={selectedServer}
						busy={busy}
						onCancel={() => setScreen({ kind: "detail", name: selectedServer.name })}
						onSubmit={(input) => void mutate(() => app.UpdateMCPServer(selectedServer.name, input)).then((ok) => { if (ok) setScreen({ kind: "detail", name: selectedServer.name }); })}
					/>
				</div>
			)}
			{screen.kind === "detail" && selectedServer && (
				<div className="cap-mcp-subpage">
					<MCPSettingsSubpageHeader title={selectedServer.name} description={t("caps.serverDetailsHint")} onBack={() => setScreen({ kind: "list" })} />
					{selectedServer.error && (
						<div className="cap-mcp-detail-error">
							<div className="banner banner--error">{summarizeServerError(selectedServer.error)}</div>
							<details>
								<summary>{t("caps.rawLog")}</summary>
								<pre>{selectedServer.error}</pre>
							</details>
						</div>
					)}
					<ServerDetails
						s={selectedServer}
						tools={selectedServer.toolList ?? []}
						busy={actionBusy}
						onConfirm={() => void mutate(() => app.RemoveMCPServer(selectedServer.name)).then((ok) => { if (ok) setScreen({ kind: "list" }); })}
						onConnectNow={() => void mutate(() => app.ReconnectMCPServer(selectedServer.name))}
						onReconnect={() => void mutate(() => app.ReconnectMCPServer(selectedServer.name))}
						onConfirmClearAuth={() => void mutate(() => app.ClearMCPServerAuthentication(selectedServer.name))}
						toolsExpanded
						editing={false}
						onEdit={() => setScreen({ kind: "edit", name: selectedServer.name })}
						onCancelEdit={() => undefined}
						onUpdate={() => undefined}
						onToggleTools={() => undefined}
						standalone
						showToolsToggle={false}
					/>
				</div>
			)}
		</section>
	);
}

// SkillsSettingsPage is a self-contained skills management page embedded inside
// the settings centre.
export function SkillsSettingsPage() {
	const t = useT();
	const [snapshotKey, setSnapshotKey] = useState("");
	const [view, setView] = useState<SkillsSettingsView | null>(null);
	const [busy, setBusy] = useState(false);
	const [err, setErr] = useState<string | null>(null);
	const [skillQuery, setSkillQuery] = useState("");
	const [expandedSkills, setExpandedSkills] = useState<Set<string>>(() => new Set());

	const reload = useCallback(async () => {
		const [meta, tabs] = await Promise.all([
			app.Meta().catch(() => null),
			app.ListTabs().catch(() => []),
		]);
		const key = settingsSnapshotKey(meta, tabs);
		setSnapshotKey(key);
		const cached = key ? skillsSettingsSnapshot : null;
		if (cached?.key === key) {
			setView(cached.value);
		} else {
			setView(null);
		}
		const next = normalizeSkillsSettingsView(await app.SkillsSettings().catch(() => ({ skills: [], skillRoots: [] })));
		skillsSettingsSnapshot = { key, value: next };
		setView(next);
	}, []);
	useEffect(() => { void reload(); }, [reload]);

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

	const filteredSkills = useMemo(() => {
		if (!view) return [];
		const q = skillQuery.trim().toLowerCase();
		if (!q) return view.skills;
		return view.skills.filter((sk) => {
			const text = [sk.name, "/" + sk.name, sk.invocation, sk.plugin, sk.description, sk.scope, sk.runAs].join(" ").toLowerCase();
			return text.includes(q);
		});
	}, [view, skillQuery]);

	const skillSummary = useMemo(() => {
		if (!view) return "";
		return skillListSummary(view.skills, filteredSkills, skillQuery.trim().length > 0, t);
	}, [filteredSkills, skillQuery, t, view]);

	const toggleSkill = useCallback((name: string) => {
		setExpandedSkills((prev) => { const next = new Set(prev); if (next.has(name)) next.delete(name); else next.add(name); return next; });
	}, []);

	if (!view) return <div className="empty">{t("caps.loading")}</div>;
	const actionBusy = busy || !snapshotKey;

	return (
		<section className="mem-section">
			{err && <div className="banner banner--error">{err}</div>}
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
				busy={actionBusy}
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
							busy={actionBusy}
							expanded={expandedSkills.has(sk.name)}
							onToggle={() => toggleSkill(sk.name)}
							onToggleEnabled={(enabled) => void mutate(() => app.SetSkillEnabled(sk.name, enabled))}
						/>
					))}
				</div>
			)}
		</section>
	);
}
