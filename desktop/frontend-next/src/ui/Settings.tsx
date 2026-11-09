import { useCallback, useEffect, useRef, useState } from "react";
import { ArrowLeft } from "lucide-react";
import { StudioIcon } from "./StudioIcon";
import { WindowControls, zoomOnTitleBar } from "./WindowControls";
import { useModalFocus } from "./useModalFocus";
import { SettingsLeaveContext, useSettingsLeave } from "./SettingsLeave";
import { t } from "../i18n";
import { listenAction } from "./listen";
import { useRuntimeReload } from "./RuntimeReload";
import type { CapabilityScope, McpEntry, ModelEntry, PluginPackage, SkillEntry } from "../port/port";
import { usePageIndex, useSettingsSearch } from "./useSettingsSearch";
import { PageIndex } from "./PageIndex";
import { useSettingLanding } from "./useSettingLanding";
import { bytes, tokens as fmtTokens } from "../i18n/format";
import { STORAGE_TALLY_DELAY, cachedStorageTotal, storageTotal } from "./storageTally";
import { SECTION_NAME, SETTING_AT, pageHome } from "./prefsnav";
import { SettingsNav, SettingsTitle } from "./SettingsNav";
import { SettingsPageStyle, SoloStyle } from "./SettingsPageStyle";
import { firstPage, pageOf } from "./settingPages";
import { Group, SCOPE_SAID } from "./Group";
import { CommonSettings } from "./CommonSettings";
import { AgentSettings } from "./AgentSettings";
import { ChatDisplaySettings } from "./ChatDisplaySettings";
import { OpsSettings } from "./OpsSettings";
import { AccountSettings } from "./AccountSettings";
import { ConnectSettings } from "./ConnectSettings";
import { SafetySettings } from "./SafetySettings";
import type { Section } from "./prefsnav";
import { AddServer } from "./AddServer";
import type { RemoteHost } from "../port/remote";
import { AddPlugin } from "./AddPlugin";
import { Packages } from "./Packages";
import { ExtAddMenu, type ExtAddWay } from "./ExtAddMenu";
import { ExtDetail, ExtSplit, firstPick } from "./ExtDetail";
import type { ExtPicked } from "./extpick";
import { ExtTabs, MarketGroup } from "./Market";
import { InstalledLocation } from "./InstalledLocation";
import { ModelSettings } from "./ModelSettings";
import { ServerRow } from "./ServerRow";
import { SkillGroups } from "./SkillRow";
import { ConfigTrouble } from "./ConfigTrouble";
import { ModHost } from "./ModHost";
import { DataSettings } from "./DataSettings";
import { activeKind, groupVendors } from "./Models";
import { useModelCatalog } from "./useModelCatalog";
import { useRoles } from "./useRoles";
import { KIND_LABEL } from "./vendors";
import { planProtocolSwitch } from "./protocolswitch";
import { Boundary } from "./Boundary";
import { DEFAULT_DAYS } from "./Usage";
import { Appearance, SCHEMES } from "./Appearance";
import { ScopeAway, ScopePill, ScopeRow } from "./CapabilityScope";
import { SectionCard } from "./SectionCard";
import { savedNotApplied, reason } from "../i18n/kernel";
import type { SettingsProps as Props } from "./SettingsProps";
import { approvalName } from "./approvals";

// An empty list hides Advanced. Only features available in the legacy app
// without a settings page here belong in this list.
const ELSEWHERE: string[] = [];

// The user's question is "is it there and does it work", so the state is the
// label. A failed server keeps its error on the row that names it.
const NET_MODE: Record<string, string> = { auto: "look.follow_the_system", env: "settings.environment_variables", custom: "about.manual", off: "models.direct" };

const homeFor = (section: Section, anchor: string): Section => {
  const entry = SETTING_AT(anchor);
  return pageHome(entry ? entry.section : section);
};

const SOLO: Record<string, Section | undefined> = { extensions: "ext", spend: "usage" };
const SOLO_TITLE: Record<string, string> = { ext: "ext.extension", usage: "app.usage" };

export function Settings({ hub, onError, port, networkPort, localWorkspacePort, networkHost, status, theme, onTheme, contrast, onContrast, weight, onWeight, look, onLook, onClose, onChanged, onSessionsRecovered, reloadThemes, at: opened, account: acct, accountUnread, reloadAccount, workspaceRoot }: Props) {
  const navigation = useSettingsLeave();
  const close = useCallback(() => navigation.leave(onClose), [navigation.leave, onClose]);
  const homePort = networkPort ?? port;
  const localPort = localWorkspacePort ?? homePort;
  const [openedName, namedAnchor = ""] = (opened ?? "").split(":", 2);
  const openedAnchor = namedAnchor || (openedName === "memory" ? "memory" : "");
  // Destinations of their own: one section drawn as a page, without the rest.
  const soloSection = SOLO[openedName];
  const solo = !!soloSection;
  const openedSection = soloSection ?? openedName;
  const [picked, setPicked] = useState<ExtPicked | null>(null);
  const [at, setAt] = useState<Section>(homeFor((openedSection as Section) || "session", openedAnchor));
  const [pages, setPages] = useState<Partial<Record<Section, string>>>(() => {
    const home = homeFor((openedSection as Section) || "session", openedAnchor);
    const first = pageOf(home, openedAnchor);
    return first ? { [home]: first } : {};
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
  const [addingPkg, setAddingPkg] = useState<false | "source" | "folder">(false);
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
      setFailed(t("settings.door_does_not_offer_model", {
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
  const remoteTally = remoteBook === null ? "" : online ? t("common.n_online", { n: online }) : remoteBook.length ? `${remoteBook.length}` : "";
  // The table of contents is also the status board: the value that matters for
  // each section rides on its own row, so the risky one is legible from here.
  const nav: Record<Section, string> = {
    common: "",
    display: "",
    controls: "",
    session: status?.plan ? t("settings.plan_mode") : preset,
    persona: "",
    model: status?.modelRef?.split("/").pop() ?? "—",
    providers: models.length ? t("settings.n_services", { n: groupVendors(models).length }) : "",
    tools: ruleCount ? `${approval} · ${ruleCount}` : approval,
    peers: "",
    hooks: hookCount ? t("common.n_2", { n: hookCount }) : t("common.off"),
    ext: broken ? t("settings.n_with_problems", { n: broken }) : packages.length ? t("settings.n_packages", { n: packages.length }) : `${looseMcp.length + looseOn}`,
    network: netMode,
    remote: remoteTally,
    memory: memCount ? t("common.n_2", { n: memCount }) : "",
    usage: tally.usage ?? "",
    storage: tally.storage ?? "",
    account: acct === null ? "" : acct.signedIn ? (acct.user?.label ?? t("settings.signed_in")) : t("common.not_signed_in"),
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
  const pageItems = usePageIndex(at, shown, remoteBook);
  // 落点：跳过去之后短暂标一下「就是这里」。这是导航反馈，不是状态变化。
  const pathOf = (section: Section) => {
    const home = pageHome(section);
    const page = t(SECTION_NAME[home] ?? home);
    return home === section ? page : `${page} › ${t(SECTION_NAME[section] ?? section)}`;
  };
  const go = (requested: Section, anchor: string) => {
    const section = homeFor(requested, anchor);
    const land = () => {
      setAt(section);
      const target = pageOf(section, anchor);
      if (target) setPages((p) => ({ ...p, [section]: target }));
      setQuery("");
      setLanded(anchor);
      if (section === "ext") setExtTab(anchor === "market" ? "market" : "installed");
    };
    if (section === at && anchor !== "persona") land();
    else navigation.leave(land);
  };
  useSettingLanding(root, landed, at, setLanded);

  return (
    <SettingsLeaveContext.Provider value={navigation.register}><div
      className="prefs prefs-screen" role="dialog" aria-modal="true" aria-labelledby="prefs-title"
      ref={root}
      tabIndex={-1}
    >
      <header className="prefs-windowbar" onDoubleClick={zoomOnTitleBar}>
        <button className="prefs-back" type="button" data-action="settings.close" disabled={updatingPkg.applying} onClick={close} aria-label={`${t("common.close")} Esc`} title={`${t("common.close")} Esc`}>
          <ArrowLeft size={16} />{t("settings.back_to_workspace")}
        </button>
        <WindowControls />
      </header>
      <div className="prefs-sheet" data-solo={soloSection}>
      <div className="prefs-hd">
        <h1 id="prefs-title">{t(solo ? SOLO_TITLE[soloSection] : "common.settings")}</h1>
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
          placeholder={t("settings.search_settings")}
          aria-label={t("settings.search_settings")}
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
        {found !== null && (
          <div className="prefs-found" role="listbox" aria-label={t("settings.search_settings")}>
            {found.length === 0 ? (
              <p className="none">{t("settings.no_matching_setting")}</p>
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
                  <span className="where">{pathOf(e.section)}</span>
                  <span className="what">{t(e.title)}</span>
                  {/* Search is where a setting is met with no page around it,
                      so it is where ownership is worth most: "代理" turning up
                      under 网络 says nothing about whether it is this project's
                      proxy or this machine's. Same catalogue as the block. */}
                  <span className="cost">
                    {[
                      t(SCOPE_SAID[e.scope]),
                      e.apply === "runtime-rebuild" ? t("settings.rebuilds_the_runtime") : e.apply === "restart" ? t("settings.takes_effect_after_a_restart") : e.apply === "on-open" ? t("settings.applies_to_new_sessions") : e.apply === "next-turn" ? t("settings.applies_from_the_next_turn") : e.apply === "next-rebuild" ? t("settings.applies_at_the_next_session") : "",
                    ].filter(Boolean).join(" · ")}
                  </span>
                </button>
              ))
            )}
          </div>
        )}
      </div>
      <div className="prefs-body">
        {!solo && <SettingsNav at={at} values={nav} shown={shown} danger={danger} disabled={updatingPkg.applying} onSelect={(section) => { if (section !== at) navigation.leave(() => { setLanded(""); setAt(section); }); }} page={pageNow} onPage={(section, page) => navigation.leave(() => { setLanded(""); setPages((p) => ({ ...p, [section]: page })); })} />}

        <div className="prefs-main" key={at} role="tabpanel" aria-labelledby={pageNow ? `prefs-${at}-${pageNow}` : `prefs-${at}`} data-sec={at}>
          <ConfigTrouble port={port} onRepaired={onChanged} />
          {!solo && <SettingsTitle at={at} />}
          {!solo && <PageIndex items={pageItems} onGo={(entry) => go(entry.section, entry.anchor)} />}
          <Boundary
            fallback={
              <div className="find" data-lvl="err" role="alert">
                <span className="t">{t("settings.this_settings_section_failed")}</span>
                <span className="why">{t("settings.other_sections_and_your_session")}</span>
              </div>
            }
          >
          {/* Keyed on the section: replacing this column is the navigation,
              and it is the one thing here whose mount really is the cause. */}
          <div className="prefs-col" key={at}>
          {solo ? <SoloStyle section={soloSection} /> : <SettingsPageStyle section={at} page={pageNow} />}
          {/* Every refused write on every page lands in one state, so it is
              said in one place. Copied onto the pages somebody remembered, it
              was missing from the third that writes it: 创建隔离副本 was
              refused by the kernel and the screen showed nothing at all. */}
          {note && (
            <div className="find" data-lvl="warn" role={note.unapplied ? "status" : "alert"}>
              <span className="t">{t(note.unapplied ? "common.saved_not_applied_yet" : "common.that_did_not_go_through")}</span>
              <span className="why">{note.text}</span>
            </div>
          )}
          {at === "session" && <AgentSettings port={port} hub={hub} persona={landed === "persona"} onPersona={() => go("session", "persona")} onChanged={onChanged} />}

          {at === "model" && (
            <ModelSettings port={port} onChanged={onChanged} onFailed={setFailed}
              usage={{
                models, roles, main: status?.modelRef, busy, protocol,
                onMain: (ref) => run(ref, async () => {
                  await port.setModel(ref);
                  await localPort.setDefaultModel(ref);
                  loadModels();
                }),
                effort: status?.effort || "auto", onEffort: (level) => run(`effort:${level}`, () => port.setEffort(level)),
                onRole: (role, ref, effort) => run(`role:${role}`, () => port.setRole(role, ref, effort).finally(loadRoles)),
                overrides, onClearOverride: (role, key) => run(`role:${role}`, () => port.clearRoleOverride(role, key).finally(loadRoles)),
              }}
              providers={{
                port, onChanged: () => { loadModels(); onChanged(); }, onFailed: setFailed, protocol,
                activeKindFor: (a) => kindFor(a.key), declare, onProtocol: (a, kind) => switchProtocol(a.key, kind),
              }} />
          )}

          {at === "tools" && (
            <>
              <SafetySettings port={port} mode={status?.toolApprovalMode} busy={busy} onChanged={onChanged}
                onMode={(id) => run(id, () => port.setApprovalMode(id))} />
              {scope && (
                <SectionCard title={t("ext.extension_scope")}>
                  <ScopeRow scope={scope} scopes={scopes} onPick={setScopeAt} />
                </SectionCard>
              )}
            </>
          )}

          {at === "display" && <ChatDisplaySettings port={port} />}
          {at === "controls" && <OpsSettings />}

          {at === "ext" && (
            <>
              <div className="ext-bar">
                <ExtTabs at={extTab} disabled={updatingPkg.applying} onPick={(tab) => { setInstalledTarget(null); setExtTab(tab); }} />
                {scope && <span className="ext-scope"><ScopePill scope={scope} scopes={scopes} onPick={setScopeAt} /></span>}
                <ExtAddMenu disabled={updatingPkg.applying} offer={new Set<ExtAddWay>(["market", ...(updatingPkg.name ? [] : ["source", "folder"] as const), ...(live ? ["service"] as const : [])])}
                  onPick={(way) => { setInstalledTarget(null); setExtTab(way === "market" ? "market" : "installed"); if (way === "service") setAdding(true); else if (way !== "market") setAddingPkg(way); }} />
              </div>
              {extTab === "installed" && <InstalledLocation root={root} target={installedTarget} packages={packages} skills={skills} mcp={mcp} refreshing={extRefreshing} onFound={setInstalledTarget} />}
              {extTab === "market" && <MarketGroup port={port} onApplying={applyingChanged} onInstalled={afterExtChange} onViewInstalled={(kind, name) => { setInstalledTarget({ kind, name }); setExtTab("installed"); }} account={acct} onSignIn={() => go("account", "account")} />}
              {extTab === "installed" && (
              <>
              <ExtSplit on={soloSection === "ext"} picked={picked ?? firstPick(packages, looseMcp, looseSkills)} onPick={setPicked} detail={
                <ExtDetail picked={picked ?? firstPick(packages, looseMcp, looseSkills)} packages={packages} mcp={mcp} skills={skills} port={port} root={scopeAt} live={live} implicit={implicit}
                  updating={updatingPkg.name} onChanged={afterExtChange} onReloadError={reload.report} onReloaded={reload.applied}
                  onUpdate={(name) => setUpdatingPkg((p) => ({ ...p, name, applying: false }))} onFailed={setFailed} />
              }>
                {scope && !scope.current && <ScopeAway scope={scope} onBack={() => setScopeAt("")} />}
                <ModHost port={port} />
                <Group id="plugins"
                  title={t("settings.plugin_packages")}
                  now={packages.length ? t("common.n", { n: packages.length }) : undefined}
                  hint={t("settings.one_package_can_bring_skills")}
                >
                  {addingPkg && (
                    <AddPlugin port={port} pickFirst={addingPkg === "folder"} onClose={() => setAddingPkg(false)} onInstalled={afterExtChange} />
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
                  {packages.length === 0 && !addingPkg && !extRefreshing && !extErrors.packages && <div className="empty">{t("settings.no_plugin_packages_installed")}</div>}
                </Group>
                {/* Below the packages: what was added by hand. A server the user
                    typed in themselves is not part of anyone's package, and
                    filing it under one would misname where it came from. */}
                <Group id="mcp"
                  title={t("ext.external_services")}
                  now={looseMcp.length ? t("settings.n_services", { n: looseMcp.length }) : undefined}
                  hint={t("settings.mcp_services_you_connected_yourself")}
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
                  {looseMcp.length === 0 && !adding && !extRefreshing && !extErrors.mcp && <div className="empty">{t("settings.no_external_services_connected")}</div>}
                </Group>
                <Group id="skills"
                  title={t("settings.skills")}
                  now={looseSkills.length ? t("settings.on_all_on", { on: looseOn, all: looseSkills.length }) : undefined}
                  hint={t(
                    implicit
                      ? "settings.skills_from_the_working_directory"
                      : "settings.model_discovery_is_off_only",
                  )}
                >
                  <SkillGroups skills={looseSkills} implicit={implicit} port={port} onDone={afterExtChange} root={scopeAt} onFailed={setFailed} />
                  {extErrors.skills && <div className="rnote" data-s="bad" role="alert">{extErrors.skills} <button className="act" data-action="extensions.refresh" disabled={extRefreshing} onClick={reloadExt}>{t("common.retry")}</button></div>}
                  {looseSkills.length === 0 && !extRefreshing && !extErrors.skills && <div className="empty">{t("settings.no_skills_in_this_working")}</div>}
                </Group>
                <div className="ext-foot" role="note">
                  <StudioIcon name="refresh" />
                  <span>{t("settings.changes_apply_from_the_next_turn")}</span>
                  {reload.action}
                </div>
                {reload.note}
              </ExtSplit>
              </>
              )}
            </>
          )}
          {at === "network" && (
            <ConnectSettings port={port} networkPort={networkPort ?? port} host={networkHost ?? ""} hub={hub} onError={onError} remote={shown("remote")} />
          )}

          {at === "account" && <AccountSettings port={port} homePort={homePort} state={acct} unread={accountUnread} reload={reloadAccount} />}
          {at === "storage" && (
            <DataSettings port={port} homePort={homePort} localPort={localPort} hub={hub} workspace={workspaceRoot ?? (homePort === port ? status?.workspaceRoot ?? "" : "")} cwd={status?.cwd} busy={busy}
              signedIn={!!acct?.signedIn && !!acct.user} onIsolate={() => run("isolate", () => port.isolateWorkspace())} onChanged={onChanged} onRecovered={() => { onChanged(); onSessionsRecovered?.(); }} />
          )}

          {at === "common" && <CommonSettings port={port} homePort={homePort} theme={theme} onTheme={onTheme} look={look} onLook={onLook} />}

          {at === "appearance" && (
            <Appearance port={homePort} theme={theme} onTheme={onTheme} contrast={contrast} onContrast={onContrast} weight={weight} onWeight={onWeight} reloadThemes={reloadThemes} look={look} onLook={onLook} />
          )}

          {at === "advanced" && (
            <Group id="elsewhere" title={t("settings.not_in_this_version_yet")} hint={t("settings.the_items_below_have_no")}>
              {ELSEWHERE.map((x) => (
                <div className="lrow" key={x}>
                  <span className="ds">{x}</span>
                  <span className="sc">{t("settings.previous_desktop_app")}</span>
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
