import { WindowControls } from "./WindowControls";
import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { onSettingsWideChange, setSettingsWide, settingsWide } from "../state/settingsWide";
import { StudioIcon } from "./StudioIcon";
import { useModalFocus } from "./useModalFocus";
import { SettingsLeaveContext, useSettingsLeave } from "./SettingsLeave";
import { t } from "../i18n";
import { listenAction } from "./listen";
import { useRuntimeReload } from "./RuntimeReload";
import type { CapabilityScope, McpEntry, ModelEntry, PluginPackage, SkillEntry } from "../port/port";
import { useSettingsSearch } from "./useSettingsSearch";
import { useSettingLanding } from "./useSettingLanding";
import { bytes, tokens as fmtTokens } from "../i18n/format";
import { STORAGE_TALLY_DELAY, cachedStorageTotal, storageTotal } from "./storageTally";
import { SECTION_NAME } from "./prefsnav";
import { SettingsNav } from "./SettingsNav";
import { SettingsPageStyle } from "./SettingsPageStyle";
import { firstPage, pageOf } from "./settingPages";
import { Group, SCOPE_SAID } from "./Group";
import { PlanApproval, DefaultMode } from "./PlanApproval";
import { PeersSection } from "./PeersSection";
import { SessionCoordination } from "./SessionCoordination";
import type { Section } from "./prefsnav";
import { AddServer } from "./AddServer";
import { PhoneAccess } from "./PhoneAccess";
import { PhoneReminders } from "./PhoneReminders";
import { remoteDeviceId } from "../port/cloud_remote";
import { PhoneNotifications } from "./PhoneNotifications";
import { Remotes } from "./Remotes";
import type { RemoteHost } from "../port/remote";
import { AddPlugin } from "./AddPlugin";
import { Packages } from "./Packages";
import { ExtTabs, MarketGroup } from "./Market";
import { InstalledLocation } from "./InstalledLocation";
import { Switch } from "./Switch";
import { ProviderHealthSetting } from "./ProviderHealthSetting";
import { ServerRow } from "./ServerRow";
import { SkillGroups } from "./SkillRow";
import { Hooks } from "./Hooks";
import { ScheduledRuns } from "./ScheduledRuns";
import { Network } from "./Network";
import { Shell as ShellPicker } from "./Shell";
import { ExternalOrigin } from "./ExternalOrigin";
import { Rules, ToolList } from "./Rules";
import { ConfigTrouble } from "./ConfigTrouble";
import { Compaction } from "./Compaction";
import { Sandbox } from "./Sandbox";
import { BrowserTools } from "./BrowserTools";
import { PresentFiles } from "./PresentFiles";
import { AskTimeout } from "./AskTimeout";
import { SessionRef } from "./SessionRef";
import { TraceExport } from "./TraceExport";
import { ModHost } from "./ModHost";
import { WebAccess } from "./WebAccess";
import { ProgressWatch } from "./ProgressWatch";
import { DisplaySettings } from "./DisplaySettings";
import { AutoArchive } from "./AutoArchive";
import { AgentPresets } from "./AgentPresets";
import { Account } from "./Account";
import { Backup } from "./Backup";
import { BackupFile } from "./BackupFile";
import { Providers } from "./Providers";
import { activeKind, groupVendors } from "./Models";
import { ModelUsage } from "./ModelUsage";
import { useModelCatalog } from "./useModelCatalog";
import { useRoles } from "./useRoles";
import { KIND_LABEL } from "./vendors";
import { planProtocolSwitch } from "./protocolswitch";
import { Boundary } from "./Boundary";
import { Path } from "./Path";
import { About } from "./About";
import { Memory } from "./Memory";
import { DEFAULT_DAYS } from "./Usage";
import { UsageSettings } from "./UsageSettings";
import { Storage } from "./Storage";
import { Appearance, SCHEMES } from "./Appearance";
import { ScopeBar } from "./CapabilityScope";
import { savedNotApplied, reason } from "../i18n/kernel";
import type { SettingsProps as Props } from "./SettingsProps";
import { APPROVALS, approvalName, approvalNote } from "./approvals";

// An empty list hides Advanced. Only features available in the legacy app
// without a settings page here belong in this list.
const ELSEWHERE: string[] = [];

// The user's question is "is it there and does it work", so the state is the
// label. A failed server keeps its error on the row that names it.
const NET_MODE: Record<string, string> = { auto: "common.followTheSystem", env: "settings.environmentVariables", custom: "common.manual", off: "common.direct" };

export function Settings({ hub, onError, port, networkPort, localWorkspacePort, networkHost, status, theme, onTheme, contrast, onContrast, weight, onWeight, look, onLook, onClose, onChanged, onSessionsRecovered, reloadThemes, at: opened, account: acct, accountUnread, reloadAccount, workspaceRoot }: Props) {
  const navigation = useSettingsLeave();
  const close = useCallback(() => navigation.leave(onClose), [navigation.leave, onClose]);
  const wide = useSyncExternalStore(onSettingsWideChange, settingsWide, settingsWide);
  const homePort = networkPort ?? port;
  const localPort = localWorkspacePort ?? homePort;
  const [openedSection, openedAnchor = ""] = (opened ?? "").split(":", 2);
  const [at, setAt] = useState<Section>((openedSection as Section) || "session");
  const [pages, setPages] = useState<Partial<Record<Section, string>>>(() => {
    const first = pageOf((openedSection as Section) || "session", openedAnchor);
    return first ? { [(openedSection as Section) || "session"]: first } : {};
  });
  const pageNow = pages[at] ?? firstPage(at);
  // 搜索的语料是这张表，不是屏幕上的 DOM：分区是按需挂载的，读 DOM 就只搜得到
  // 当前这一页，而为了搜索把每一页都挂起来会让隐藏页面开始发请求。
  const [query, setQuery] = useState("");
  const [landed, setLanded] = useState(openedAnchor);
  // Opened from the composer's effort menu: the running source's editor opens
  // on its reasoning fields. The model ref's first segment is the source name.
  const declare = openedAnchor === "effort-declare" ? status?.modelRef?.split("/")[0] : undefined;
  const [models, setModels] = useState<ModelEntry[]>([]);
  const { roles, overrides, loadRoles } = useRoles(port);
  const [protocol, setProtocol] = useState<Record<string, string>>({});
  const [mcp, setMcp] = useState<McpEntry[]>([]);
  const [scope, setScope] = useState<CapabilityScope | null>(null);
  const [scopes, setScopes] = useState<CapabilityScope[]>([]);
  // Which project the extension tab is answering for. Empty is the running one,
  // so the common case stays exactly what it was.
  const [scopeAt, setScopeAt] = useState("");
  const [skills, setSkills] = useState<SkillEntry[]>([]);
  const [implicit, setImplicit] = useState(true);
  const [live, setLive] = useState(true);
  const [busy, setBusy] = useState("");
  const [note, setNote] = useState<{ text: string; unapplied: boolean } | null>(null);
  const setFailed = useCallback((text: string) => setNote(text ? { text, unapplied: false } : null), []);
  const [adding, setAdding] = useState(false);
  const [remoteBook, setRemoteBook] = useState<RemoteHost[] | null>(null);
  const [packages, setPackages] = useState<PluginPackage[]>([]);
  const [addingPkg, setAddingPkg] = useState(false);
  const [extTab, setExtTab] = useState<"installed" | "market">(openedAnchor === "market" ? "market" : "installed");
  const [installedTarget, setInstalledTarget] = useState<{ kind: string; name: string } | null>(null);
  const [extRefreshing, setExtRefreshing] = useState(false);
  const [extErrors, setExtErrors] = useState<Partial<Record<"mcp" | "packages" | "skills", string>>>({});
  const extRefresh = useRef(0);
  const [updatingPkg, setUpdatingPkg] = useState({ connection: { port }, name: "", applying: false });
  if (updatingPkg.connection.port !== port) setUpdatingPkg({ connection: { port }, name: "", applying: false });
  const applyingChanged = useCallback((applying: boolean) => {
    setUpdatingPkg((p) => p.connection === updatingPkg.connection ? { ...p, applying } : p);
  }, [updatingPkg.connection]);
  const [hookCount, setHookCount] = useState(0);
  const [netMode, setNetMode] = useState("");
  const [memCount, setMemCount] = useState(0);
  const [ruleCount, setRuleCount] = useState(0);
  const root = useRef<HTMLDivElement>(null);
  useModalFocus(root);
  const veiled = useRef(false);
  const reloadExt = useCallback(() => {
    const refresh = ++extRefresh.current;
    const current = () => extRefresh.current === refresh;
    setExtRefreshing(true);
    setExtErrors({});
    const where = scopeAt || undefined;
    port.capabilityScopes().then((c) => { if (current()) setScopes(c); }).catch(() => { if (current()) setScopes([]); });
    const mcpRead = port.mcp(where).then((c) => {
      if (!current()) return;
      setMcp(c.servers);
      setScope(c.scope);
      setLive(c.live !== false);
    }).catch((e) => { if (current()) { setMcp([]); setExtErrors((errors) => ({ ...errors, mcp: reason(e) })); } });
    const packageRead = port.plugins().then((c) => { if (current()) setPackages(c); }).catch((e) => { if (current()) { setPackages([]); setExtErrors((errors) => ({ ...errors, packages: reason(e) })); } });
    port.hooks().then((c) => { if (current()) setHookCount(c.hooks.length); }).catch(() => { if (current()) setHookCount(0); });
    homePort.network().then((n) => { if (current()) setNetMode(t(NET_MODE[n.mode] ?? n.mode)); }).catch(() => { if (current()) setNetMode(""); });
    localPort.memories().then((c) => { if (current()) setMemCount(c.memories.length); }).catch(() => { if (current()) setMemCount(0); });
    port.permissions().then((p) => { if (current()) setRuleCount(p.deny.length + p.ask.length + p.allow.length); }).catch(() => { if (current()) setRuleCount(0); });
    const skillRead = port.skills(where).then((c) => {
      if (!current()) return;
      setSkills(c.skills);
      setImplicit(c.implicit);
    })
      .catch((e) => { if (current()) { setSkills([]); setExtErrors((errors) => ({ ...errors, skills: reason(e) })); } });
    void Promise.allSettled([mcpRead, packageRead, skillRead]).then(() => { if (current()) setExtRefreshing(false); });
  }, [port, homePort, localPort, scopeAt]);
  // Adding or removing a source changes what the picker above can offer, so
  // the list is reloadable rather than read once at mount.
  const loadModels = useModelCatalog(port, localPort, setModels);

  const currentExt = useRef({ port, reloadExt, loadModels, onChanged });
  currentExt.current = { port, reloadExt, loadModels, onChanged };
  const afterExtChange = useCallback(() => {
    if (currentExt.current.port !== port) return;
    currentExt.current.reloadExt();
    currentExt.current.loadModels();
    currentExt.current.onChanged();
  }, [port]);
  const reload = useRuntimeReload(port, afterExtChange);

  // Three sections whose row used to report nothing. Loaded here rather than in
  // reloadExt because none of them moves with the scope the extension lists are
  // read at, and dropped on failure: a table of contents that says "—" where a
  // number failed to arrive has invented a fact about the section.
  const [tally, setTally] = useState<Partial<Record<Section, string>>>({});
  useEffect(() => {
    let alive = true;
    setTally({});
    const put = (k: Section, v: string) => { if (alive) setTally((prev) => ({ ...prev, [k]: v })); };
    const known = cachedStorageTotal(homePort);
    if (known !== undefined) put("storage", bytes(known));
    const walk = known !== undefined ? undefined : setTimeout(() => { storageTotal(homePort).then((n) => put("storage", bytes(n))).catch(() => {}); }, STORAGE_TALLY_DELAY);
    // The same window Usage itself opens on, or the list and the page it opens
    // would report two different totals for one word.
    homePort.usage(DEFAULT_DAYS).then((u) => put("usage", fmtTokens(u.tokens))).catch(() => {});
    homePort.versions().then((v) => put("versions", v.current || "dev")).catch(() => {});
    return () => { alive = false; clearTimeout(walk); };
  }, [homePort]);

  // Focus lands once, when the pane opens, and goes back where it came from on
  // the way out. onClose is a fresh arrow on every parent render, so re-running
  // this with it would pull focus out of whatever the user is holding — a
  // native select closes its dropdown the instant it is blurred, which reads as
  // the menu refusing to open. Closing used to leave focus on <body>, which
  // left a reader on the keyboard with no position at all.
  useEffect(() => {
    const from = document.activeElement as HTMLElement | null;
    root.current?.focus();
    return () => {
      if (from?.isConnected) from.focus();
    };
  }, []);

  useEffect(() => {
    const onKey = (e: Event) => (e as KeyboardEvent).key === "Escape" && !updatingPkg.applying && close();
    return listenAction(window, "keydown", { action: "settings.close", listener: onKey });
  }, [close, updatingPkg.applying]);

  useEffect(() => {
    loadModels();
    loadRoles();
    reloadExt();
  }, [loadModels, loadRoles, reloadExt]);

  // Read once for the tab's own count. Remotes keeps its own copy: it also
  // needs the ssh_config candidates, which a tab label has no use for.
  useEffect(() => {
    hub
      .remoteHosts()
      .then(setRemoteBook)
      .catch(() => setRemoteBook(null));
  }, [hub]);

  // A refused switch has to say so. The kernel turns one down while a turn or a
  // background job is running, and swallowing that leaves the click looking like
  // the row simply does not work.
  const run = async (what: string, fn: () => Promise<void>) => {
    setBusy(what);
    setNote(null);
    try {
      await fn();
      onChanged();
    } catch (e) {
      setNote({ text: reason(e), unapplied: savedNotApplied(e) });
    } finally {
      setBusy("");
    }
  };

  // The levels the selected model's endpoint actually accepts. A fixed list
  // here offered every model six of them and let the user set depths the
  // provider then ignored or rejected.
  const vendors = groupVendors(models);
  const kindFor = (key: string) => {
    const v = vendors.find((x) => x.key === key);
    return v ? activeKind(v, status?.modelRef) : "";
  };
  const switchProtocol = (key: string, kind: string) => {
    const plan = planProtocolSwitch(vendors.find((x) => x.key === key), models, status?.modelRef, kind);
    if (plan.do === "stay") {
      setFailed(t("settings.doesNotOfferChooseOne", {
        door: t(KIND_LABEL[kind] ?? kind),
        model: plan.model,
      }));
      return;
    }
    setProtocol((p) => ({ ...p, [key]: kind }));
    if (plan.do === "switch") run(plan.ref, () => port.setModel(plan.ref));
  };
  const preset = t("common.auto");
  const approval = approvalName(status?.toolApprovalMode, "—");
  const broken = mcp.filter((m) => m.state === "failed").length;
  // A package owns what it brought, and its own row already lists it. What is
  // left is what the user added by hand, which is the only thing these two
  // lists can act on without contradicting the package above them. Ownership
  // is read off the packages themselves: a live server reports the config
  // layer it was merged from, which is not the same question.
  const owned = new Set(packages.flatMap((p) => p.mcpServers?.map((s) => s.name) ?? []));
  const looseMcp = mcp.filter((m) => !owned.has(m.name));
  const looseSkills = skills.filter((s) => !s.plugin);
  const looseOn = looseSkills.filter((s) => s.enabled).length;
  // Null means this kernel does not do remote panes at all, which is what
  // takes the whole tab out rather than showing an empty one.
  const online = (remoteBook ?? []).filter((h) => h.status === "connected" || h.status === "degraded").length;
  const remoteTally = remoteBook === null ? "" : online ? t("settings.online", { n: online }) : remoteBook.length ? `${remoteBook.length}` : "";
  // The table of contents is also the status board: the value that matters for
  // each section rides on its own row, so the risky one is legible from here.
  const nav: Record<Section, string> = {
    session: status?.plan ? t("common.planMode") : preset,
    persona: "",
    model: status?.modelRef?.split("/").pop() ?? "—",
    providers: models.length ? t("common.services", { n: groupVendors(models).length }) : "",
    tools: ruleCount ? `${approval} · ${ruleCount}` : approval,
    peers: "",
    hooks: hookCount ? t("common.text3", { n: hookCount }) : t("common.off"),
    ext: broken ? t("settings.withProblems", { n: broken }) : packages.length ? t("settings.packages", { n: packages.length }) : `${looseMcp.length + looseOn}`,
    network: netMode,
    remote: remoteTally,
    memory: memCount ? t("common.text3", { n: memCount }) : "",
    usage: tally.usage ?? "",
    storage: tally.storage ?? "",
    account: acct === null ? "" : acct.signedIn ? (acct.user?.label ?? t("common.signedIn")) : t("common.notSignedIn"),
    versions: tally.versions ?? "",
    appearance: t(SCHEMES.find(([id]) => id === theme)?.[1] ?? ""),
    advanced: ELSEWHERE.length ? `${ELSEWHERE.length}` : "",
  };
  const danger = (id: Section) =>
    (id === "tools" && status?.toolApprovalMode === "yolo") || (id === "ext" && broken > 0);
  // Null remote means this kernel does not do remote panes; advanced holds
  // whatever has moved out of the config file and has not moved in yet.
  const shown = (id: Section) =>
    (id !== "remote" || remoteBook !== null) && (id !== "advanced" || ELSEWHERE.length > 0);

  // 搜索命中的是这张表里的条目，包括当前没有挂载的那些分区；能不能显示由 shown
  // 决定，和它有没有被渲染过无关。
  const found = useSettingsSearch(query, shown, remoteBook);
  // 落点：跳过去之后短暂标一下「就是这里」。这是导航反馈，不是状态变化。
  const go = (section: Section, anchor: string) => {
    navigation.leave(() => {
      setAt(section);
      const target = pageOf(section, anchor);
      if (target) setPages((p) => ({ ...p, [section]: target }));
      setQuery("");
      setLanded(anchor);
      if (section === "ext") setExtTab(anchor === "market" ? "market" : "installed");
    });
  };
  useSettingLanding(root, landed, at, setLanded);

  return (
    <SettingsLeaveContext.Provider value={navigation.register}><div
      className="prefs"
      ref={root}
      tabIndex={-1}
      onMouseDown={(e) => { veiled.current = e.target === e.currentTarget; }}
      // Both ends of the press have to land on the veil. A drag that started
      // inside the sheet and let go outside is a selection, not a dismissal.
      onMouseUp={(e) => { if (veiled.current && e.target === e.currentTarget && !updatingPkg.applying) close(); }}
      data-action-mouseup="settings.close"
    >
      <div className="prefs-sheet" role="dialog" aria-modal="true" aria-labelledby="prefs-title" data-wide={wide || undefined}>
      <div className="prefs-hd">
        <h1 id="prefs-title">{t("common.settings")}</h1>
        {/* 原来这里挂着一句「改动立刻生效；需要重建运行时的……」。它对三档里的
            两档说了话，而且要读的人自己判断手上这一项属于哪一档 —— 那句话现在
            落在每一个设置块自己身上。 */}
        <input
          className="prefs-find"
          value={query}
          // Typing filters and Escape takes the filter back: two events on one
          // box, both operating the same one thing.
          data-action-change="settings.search"
          data-action-keydown="settings.search"
          placeholder={t("settings.searchSettings")}
          aria-label={t("settings.searchSettings2")}
          spellCheck={false}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            // 一层一层退：有搜索就先退搜索，没有才退整个设置。
            if (e.key === "Escape" && query) {
              e.stopPropagation();
              setQuery("");
            }
          }}
        />
        <button className="btn sm" type="button" data-action="settings.expand" aria-pressed={wide} aria-label={t("settings.enlargeTheSettingsWindow")} title={t(wide ? "settings.shrinkTheSettingsWindow" : "settings.enlargeTheSettingsWindow")} onClick={() => setSettingsWide(!wide)}>
          <StudioIcon name={wide ? "collapseAll" : "expand"} />
        </button>
        <button className="btn sm" data-action="settings.close" disabled={updatingPkg.applying} onClick={close}>
          {t("common.close")} <span className="esc">Esc</span>
        </button>
        {wide && <WindowControls />}
        {found !== null && (
          <div className="prefs-found" role="listbox" aria-label={t("settings.searchSettings2")}>
            {found.length === 0 ? (
              <p className="none">{t("settings.noMatchingSetting")}</p>
            ) : (
              found.map((e) => (
                <button
                  key={e.anchor}
                  role="option"
                  aria-selected={false} disabled={updatingPkg.applying}
                  data-action="settings.section"
                  data-value={e.section}
                  data-target={e.anchor}
                  onClick={() => go(e.section, e.anchor)}
                >
                  <span className="where">{t(SECTION_NAME[e.section] ?? e.section)}</span>
                  <span className="what">{t(e.title)}</span>
                  {/* Search is where a setting is met with no page around it,
                      so it is where ownership is worth most: "代理" turning up
                      under 网络 says nothing about whether it is this project's
                      proxy or this machine's. Same catalogue as the block. */}
                  <span className="cost">
                    {[
                      t(SCOPE_SAID[e.scope]),
                      e.apply === "runtime-rebuild" ? t("common.rebuildsTheRuntime") : e.apply === "restart" ? t("settings.takesEffectAfterARestart") : e.apply === "on-open" ? t("settings.appliesToNewSessions") : e.apply === "next-turn" ? t("settings.appliesFromTheNextTurn") : e.apply === "next-rebuild" ? t("settings.appliesAtTheNextSession") : "",
                    ].filter(Boolean).join(" · ")}
                  </span>
                </button>
              ))
            )}
          </div>
        )}
      </div>
      <ConfigTrouble port={port} onRepaired={onChanged} />
      <div className="prefs-body">
        <SettingsNav at={at} values={nav} shown={shown} danger={danger} disabled={updatingPkg.applying} onSelect={(section) => { if (section !== at) navigation.leave(() => { setLanded(""); setAt(section); }); }} page={pageNow} onPage={(section, page) => navigation.leave(() => { setLanded(""); setPages((p) => ({ ...p, [section]: page })); })} />

        <div className="prefs-main" key={at} role="tabpanel" aria-labelledby={pageNow ? `prefs-${at}-${pageNow}` : `prefs-${at}`} data-sec={at}>
          <Boundary
            fallback={
              <div className="find" data-lvl="err" role="alert">
                <span className="t">{t("settings.thisSettingsSectionFailed")}</span>
                <span className="why">{t("settings.otherSectionsAndYourSession")}</span>
              </div>
            }
          >
          {/* Keyed on the section: replacing this column is the navigation,
              and it is the one thing here whose mount really is the cause. */}
          <div className="prefs-col" key={at}>
          <SettingsPageStyle section={at} page={pageNow} />
          {/* Every refused write on every page lands in one state, so it is
              said in one place. Copied onto the pages somebody remembered, it
              was missing from the third that writes it: 创建隔离副本 was
              refused by the kernel and the screen showed nothing at all. */}
          {note && (
            <div className="find" data-lvl="warn" role={note.unapplied ? "status" : "alert"}>
              <span className="t">{t(note.unapplied ? "common.savedNotAppliedYet" : "common.thatDidNotGoThrough")}</span>
              <span className="why">{note.text}</span>
            </div>
          )}
          {at === "session" && (
            <>
              <Group id="preset" title={t("common.completionStandard")} now={preset} hint={t("settings.reasonixChoosesTheNeededVerification")}>
                <p className="note">{t("settings.everydayQuestionsStayLightweightFile")}</p>
              </Group>
              {/* An on/off that reverses by clicking again is a switch, not two
                  options — the same control the recipes and the sandbox's
                  network egress use. */}
              <Group id="plan-mode" title={t("common.planMode")} hint={t("settings.whileThisIsOnThe")}>
                <div className="lrow">
                  <span className="tx">
                    <span className="lb">{t("common.draftsAPlanReadOnly")}</span>
                    <span className="ds">
                      {t(status?.plan ? "settings.readsAndDraftsAPlan" : "settings.normalExecutionApprovedWritesAre")}
                    </span>
                  </span>
                  <Switch
                    data-action="plan.mode"
                    on={status?.plan === true}
                    busy={busy === "plan"}
                    label={t("common.planMode")}
                    onClick={() => run("plan", () => port.setPlanMode(!status?.plan))}
                  />
                </div>
              </Group><DefaultMode port={port} />
              <DisplaySettings port={port} />
              <AskTimeout port={port} />
              <SessionRef port={port} />
              <ProgressWatch port={port} />
              <AutoArchive port={port} />
              <Group id="session-dir" title={t("common.whereThisSessionWrites")}>
                <div className="kv">
                  <span className="k">{t("common.workingDirectory")}</span>
                  <span className="v"><Path of={status?.cwd ?? "—"} /></span>
                </div>
                <p className="note">
                  {t("settings.foldersAreManagedInThe")}
                </p>
                {/* A thing that happens once, not a state to sit in — so it is
                    a button. As an option row it carried a selected look it can
                    never have. */}
                <div className="lrow">
                  <span className="tx">
                    <span className="lb">{t("settings.workOnAnIsolatedCopy")}</span>
                    <span className="ds">{t("settings.workInASeparateGit")}</span>
                  </span>
                  <button className="act" data-action="workspace.isolate" disabled={!!busy} onClick={() => run("isolate", () => port.isolateWorkspace())}>
                    {t(busy === "isolate" ? "settings.opening" : "settings.openOne")}
                  </button>
                </div>
              </Group>
            </>
          )}

          {at === "persona" && <AgentPresets port={port} focusPersona={landed === "persona"} />}

          {at === "providers" && (
            <>
            <ProviderHealthSetting port={port} onChanged={onChanged} onFailed={setFailed} />
            <Group id="providers" title={t("common.modelServices")}
              hint={t("settings.pickAServiceToSee")}>
              <Providers port={port} onChanged={() => { loadModels(); onChanged(); }} onFailed={setFailed} protocol={protocol}
                activeKindFor={(a) => kindFor(a.key)}
                declare={declare}
                onProtocol={(a, kind) => switchProtocol(a.key, kind)} />
            </Group>
            </>
          )}

          {at === "model" && (
            <>
              <Group id="model" title={t("common.modelsByUse")} now={nav.model}
                hint={t("settings.chooseAModelAndReasoning")}>
                <ModelUsage models={models} roles={roles} main={status?.modelRef} busy={busy} protocol={protocol}
                  onMain={(ref) => run(ref, async () => {
                    await port.setModel(ref);
                    await localPort.setDefaultModel(ref);
                    // The row reads the catalogue's default, so the list has to
                    // be re-read or the controlled select snaps back.
                    loadModels();
                  })}
                  effort={status?.effort || "auto"} onEffort={(level) => run(`effort:${level}`, () => port.setEffort(level))}
                  onRole={(role, ref, effort) => run(`role:${role}`, () => port.setRole(role, ref, effort).finally(loadRoles))}
                  overrides={overrides} onClearOverride={(role, key) => run(`role:${role}`, () => port.clearRoleOverride(role, key).finally(loadRoles))} />
              </Group>
              <Group id="context" title={t("common.contextMaintenance")}
                hint={t("settings.compactsAutomaticallyFromModelCapaci")}>
                <Compaction port={port} />
              </Group>
            </>
          )}

          {at === "tools" && (
            <>
              <Group id="approval" title={t("common.toolApprovals")} now={approval} hint={t("settings.thisIsTheOnlyApproval")}>
                {/* Four rows of label-and-description was 190px for one choice,
                    and it was the only choice in this pane shaped that way. The
                    description follows the selection instead: what a档 does is
                    read when it is picked, not compared four at a time. */}
                <div className="seg" data-text data-danger={status?.toolApprovalMode === "yolo" ? "" : undefined}
                  role="radiogroup" aria-label={t("common.toolApprovals")}>
                  {APPROVALS.map(([id, name]) => (
                    <button key={id} role="radio" data-action="tool-approval.mode" data-value={id}
                      aria-checked={status?.toolApprovalMode === id} disabled={!!busy}
                      onClick={() => run(id, () => port.setApprovalMode(id))}>
                      {t(name)}
                    </button>
                  ))}
                </div>
                <p className="note">{approvalNote(status?.toolApprovalMode)}</p>
              </Group>
              <Group id="present-files"
                title={t("common.aIFileDeliveryCards")}
                hint={t("settings.filePreviewsRemainAvailableFrom")}
              >
                <PresentFiles port={port} />
              </Group>
              <Group id="external-origin"
                title={t("common.outsideText")}
                hint={t("settings.textInWebPagesAnd")}
              >
                <ExternalOrigin port={port} onChanged={onChanged} />
              </Group>
              <Group id="plan-approval"
                title={t("common.planApproval")}
                hint={t("settings.afterPlanModeSubmitsA")}
              >
                <PlanApproval port={port} />
              </Group>
              <Group id="rules"
                title={t("common.rulesYouSpellOut")}
                hint={t("settings.theSettingAboveDecidesWhether")}
              >
                <Rules port={port} onChanged={onChanged} />
              </Group><ToolList port={port} />
              <Group id="sandbox"
                title={t("common.sandbox")}
                hint={t("settings.howFarAnApprovedAction")}
              >
                <Sandbox port={port} onChanged={onChanged} />
              </Group>
              <Group id="shell"
                title={t("common.whichProgramRunsCommands")}
                hint={t("settings.everyCommandTheAgentRuns")}
              >
                <ShellPicker port={port} onChanged={onChanged} />
              </Group>
              <Group id="browser-tools"
                title={t("common.builtInBrowser")}
                hint={t("settings.theToolsTheAgentUses")}
              >
                <BrowserTools port={port} onChanged={onChanged} />
              </Group>
              <WebAccess port={port} />
              <SessionCoordination port={port} onChanged={onChanged} />
            </>
          )}

          {at === "peers" && <PeersSection port={port} hub={hub} />}

          {at === "hooks" && (
            <>
              <Group id="hooks"
                title={t("common.automation")}
                hint={t("settings.runYourOwnCommandsBefore")}
              >
                <Hooks port={port} onChanged={afterExtChange} />
              </Group>
              <ScheduledRuns port={port} />
            </>
          )}
          {at === "ext" && (
            <>
              <ExtTabs at={extTab} disabled={updatingPkg.applying} onPick={(tab) => { setInstalledTarget(null); setExtTab(tab); }} />
              {extTab === "installed" && <InstalledLocation root={root} target={installedTarget} packages={packages} skills={skills} mcp={mcp} refreshing={extRefreshing} onFound={setInstalledTarget} />}
              {extTab === "market" && <MarketGroup port={port} onApplying={applyingChanged} onInstalled={afterExtChange} onViewInstalled={(kind, name) => { setInstalledTarget({ kind, name }); setExtTab("installed"); }} account={acct} onSignIn={() => go("account", "account")} />}
              {extTab === "installed" && (
              <>
                {scope && <ScopeBar scope={scope} scopes={scopes} onPick={setScopeAt} />}
                {/* 装完、改完、删完都要过这一步才算数——把它放在包列表上面，
                    因为它管的是整个运行时，不是某一个包。 */}
                <Group id="ext-runtime"
                  title={t("common.runtime")}
                  hint={t("settings.useThisAfterEditingExtension")}
                  action={reload.action}
                >
                  {reload.note}
                </Group>
                <ModHost port={port} />
                <Group id="plugins"
                  title={t("common.pluginPackages")}
                  now={packages.length ? t("common.text2", { n: packages.length }) : undefined}
                  hint={t("settings.onePackageCanBringSkills")}
                  action={
                    addingPkg ? undefined : (
                      <button className="act" data-action="extensions.add" disabled={!!updatingPkg.name} onClick={() => setAddingPkg(true)}>
                        {t("common.add")}
                      </button>
                    )
                  }
                >
                  {addingPkg && (
                    <AddPlugin port={port} onClose={() => setAddingPkg(false)} onInstalled={afterExtChange} />
                  )}
                  {updatingPkg.name && (
                    <AddPlugin
                      port={port}
                      updating={packages.find((p) => p.name === updatingPkg.name)}
                      onApplying={applyingChanged}
                      onClose={() => setUpdatingPkg((p) => ({ ...p, name: "", applying: false }))} onInstalled={afterExtChange}
                    />
                  )}
                  <Packages
                    port={port}
                    packages={packages}
                    onChanged={afterExtChange} onReloadError={reload.report} onReloaded={reload.applied}
                    updating={updatingPkg.name}
                    onUpdate={(name) => setUpdatingPkg((p) => ({ ...p, name, applying: false }))}
                  />
                  {extErrors.packages && <div className="rnote" data-s="bad" role="alert">{extErrors.packages} <button className="act" data-action="extensions.refresh" disabled={extRefreshing} onClick={reloadExt}>{t("common.retry")}</button></div>}
                  {packages.length === 0 && !addingPkg && !extRefreshing && !extErrors.packages && <div className="empty">{t("settings.noPluginPackagesInstalled")}</div>}
                </Group>
                {/* Below the packages: what was added by hand. A server the user
                    typed in themselves is not part of anyone's package, and
                    filing it under one would misname where it came from. */}
                <Group id="mcp"
                  title={t("common.externalTools")}
                  now={looseMcp.length ? t("common.services", { n: looseMcp.length }) : undefined}
                  hint={t("settings.mCPServicesYouConnectedYourself")}
                  action={
                    adding || !live ? undefined : (
                      <button className="act" onClick={() => setAdding(true)}>
                        {t("settings.connectAService")}
                      </button>
                    )
                  }
                >
                  {adding && live && (
                    <AddServer
                      port={port}
                      canProject={!!status?.workspaceRoot}
                      onClose={() => setAdding(false)}
                      onInstalled={afterExtChange}
                    />
                  )}
                  {looseMcp.map((m) => (
                    <ServerRow key={m.name} m={m} port={port} onDone={afterExtChange} root={scopeAt} live={live} />
                  ))}
                  {extErrors.mcp && <div className="rnote" data-s="bad" role="alert">{extErrors.mcp} <button className="act" data-action="extensions.refresh" disabled={extRefreshing} onClick={reloadExt}>{t("common.retry")}</button></div>}
                  {looseMcp.length === 0 && !adding && !extRefreshing && !extErrors.mcp && <div className="empty">{t("settings.noExternalServicesConnected")}</div>}
                </Group>
                <Group id="skills"
                  title={t("common.skills2")}
                  now={looseSkills.length ? t("settings.on", { on: looseOn, all: looseSkills.length }) : undefined}
                  hint={t(
                    implicit
                      ? "settings.skillsFromTheWorkingDirectory"
                      : "settings.modelDiscoveryIsOffOnly",
                  )}
                >
                  <SkillGroups skills={looseSkills} implicit={implicit} port={port} onDone={afterExtChange} root={scopeAt} onFailed={setFailed} />
                  {extErrors.skills && <div className="rnote" data-s="bad" role="alert">{extErrors.skills} <button className="act" data-action="extensions.refresh" disabled={extRefreshing} onClick={reloadExt}>{t("common.retry")}</button></div>}
                  {looseSkills.length === 0 && !extRefreshing && !extErrors.skills && <div className="empty">{t("settings.noSkillsInThisWorking")}</div>}
                </Group>
              </>
              )}
            </>
          )}
          {at === "network" && (
            <Group id="network"
              title={t("common.network")}
              hint={t("settings.modelRequestsRemoteMCPServices")}
            >
              <Network port={networkPort ?? port} host={networkHost ?? ""} />
            </Group>
          )}
          {at === "network" && <TraceExport port={port} />}

          {at === "remote" && (
            <Group id="remote"
              title={t("common.remote")}
              hint={t("settings.connectAWorkspaceOnAnother")}
            >
              <Remotes hub={hub} onError={onError} />
            </Group>
          )}
          {at === "remote" && <PhoneAccess hub={hub} onError={onError} />}
          {at === "remote" && <PhoneNotifications hub={hub} port={port} onError={onError} />}
          {at === "remote" && remoteDeviceId() && <PhoneReminders host={remoteDeviceId()!} />}

          {at === "account" && (
            <Group id="account"
              title={t("common.account")}
              hint={t("settings.reasonixItselfNeedsNoAccount")}
            >
              <Account port={port} state={acct} unread={accountUnread} reload={reloadAccount} />
            </Group>
          )}
          {at === "account" && <Group id="backup-file" title={t("common.exportAndImportConfig")} hint={t("settings.noAccountNeededExportThis")}><BackupFile port={port} /></Group>}
          {at === "account" && acct?.signedIn && acct.user && (
            <Group id="backup" title={t("common.cloudBackup")} hint={t("settings.encryptThisMachineSModels")}>
              <Backup port={port} />
            </Group>
          )}
          {at === "versions" && <About port={homePort} />}
          {at === "memory" && (
            <Group id="memory"
              title={t("common.memory2")}
              hint={t("settings.whatTheAgentWroteDown")}
            >
              <Memory port={localPort} />
            </Group>
          )}

          {at === "usage" && <UsageSettings port={homePort} onChanged={onChanged} />}

          {at === "storage" && (
            <Group id="storage" title={t("common.storage")} hint={t("settings.whereTheDataIsWritten")}><Storage port={homePort} hub={hub} workspace={workspaceRoot ?? (homePort === port ? status?.workspaceRoot ?? "" : "")} onRecovered={() => { onChanged(); onSessionsRecovered?.(); }} /></Group>
          )}

          {at === "appearance" && (
            <Appearance port={homePort} theme={theme} onTheme={onTheme} contrast={contrast} onContrast={onContrast} weight={weight} onWeight={onWeight} reloadThemes={reloadThemes} look={look} onLook={onLook} />
          )}

          {at === "advanced" && (
            <Group id="elsewhere" title={t("common.notInThisVersionYet")} hint={t("settings.theItemsBelowHaveNo")}>
              {ELSEWHERE.map((x) => (
                <div className="lrow" key={x}>
                  <span className="ds">{x}</span>
                  <span className="sc">{t("settings.previousDesktopApp")}</span>
                </div>
              ))}
            </Group>
          )}
          </div>
        </Boundary>
        </div>
      </div>
      </div>
    </div></SettingsLeaveContext.Provider>
  );
}
