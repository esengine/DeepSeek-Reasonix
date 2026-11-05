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
import { LOOK_FAMILY, SECTION_NAME, SETTING_AT, pageFor, pageHome } from "./prefsnav";
import { SettingsNav, SettingsTitle } from "./SettingsNav";
import { SettingsPageStyle } from "./SettingsPageStyle";
import { firstPage, pageOf } from "./settingPages";
import { Group, SCOPE_SAID } from "./Group";
import { PlanApproval } from "./PlanApproval";
import { CommonSettings } from "./CommonSettings";
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
import { ExtAddMenu, type ExtAddWay } from "./ExtAddMenu";
import { ExtDetail, ExtSplit, firstPick } from "./ExtDetail";
import type { ExtPicked } from "./extpick";
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
import { SectionCard } from "./SectionCard";
import { DataSettings } from "./DataSettings";
import { AgentPresets } from "./AgentPresets";
import { SettingDeep, SettingDeepContext } from "./SettingDeep";
import { Account } from "./Account";
import { Providers } from "./Providers";
import { activeKind, groupVendors } from "./Models";
import { ModelUsage } from "./ModelUsage";
import { useModelCatalog } from "./useModelCatalog";
import { useRoles } from "./useRoles";
import { KIND_LABEL } from "./vendors";
import { planProtocolSwitch } from "./protocolswitch";
import { Boundary } from "./Boundary";
import { About } from "./About";
import { DEFAULT_DAYS } from "./Usage";
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
const NET_MODE: Record<string, string> = { auto: "look.follow_the_system", env: "settings.environment_variables", custom: "about.manual", off: "models.direct" };

const TWO_COLUMN: readonly Section[] = ["session", "tools", "ext", "display", "controls", "appearance", "storage"];

const homeFor = (section: Section, anchor: string): Section => {
  const entry = SETTING_AT(anchor);
  return pageHome(entry ? entry.section : section);
};

export function Settings({ hub, onError, port, networkPort, localWorkspacePort, networkHost, status, theme, onTheme, contrast, onContrast, weight, onWeight, look, onLook, onClose, onChanged, onSessionsRecovered, reloadThemes, at: opened, account: acct, accountUnread, reloadAccount, workspaceRoot }: Props) {
  const navigation = useSettingsLeave();
  const close = useCallback(() => navigation.leave(onClose), [navigation.leave, onClose]);
  const wide = useSyncExternalStore(onSettingsWideChange, settingsWide, settingsWide);
  const homePort = networkPort ?? port;
  const localPort = localWorkspacePort ?? homePort;
  const [openedName, openedAnchor = ""] = (opened ?? "").split(":", 2);
  // "extensions" is a destination of its own: the ext section drawn as a page.
  const solo = openedName === "extensions";
  const openedSection = solo ? "ext" : openedName;
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
  const [deepAt, setDeep] = useState<string | null>(() => pageFor(openedAnchor));
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
  // 落点：跳过去之后短暂标一下「就是这里」。这是导航反馈，不是状态变化。
  const pathOf = (section: Section) => {
    const home = pageHome(section);
    const page = t(SECTION_NAME[home] ?? home);
    return home === section ? page : `${page} › ${t(SECTION_NAME[section] ?? section)}`;
  };
  const go = (requested: Section, anchor: string) => {
    const section = homeFor(requested, anchor);
    navigation.leave(() => {
      setAt(section);
      setDeep(pageFor(anchor));
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
      <div className="prefs-sheet" role="dialog" aria-modal="true" aria-labelledby="prefs-title" data-wide={wide || undefined} data-solo={solo ? "ext" : undefined}>
      <div className="prefs-hd">
        <h1 id="prefs-title">{t(solo ? "ext.extension" : "common.settings")}</h1>
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
        <div className="prefs-actions">
        <button className="btn sm" type="button" data-action="settings.expand" aria-pressed={wide} aria-label={t("settings.enlarge_the_settings_window")} title={t(wide ? "settings.shrink_the_settings_window" : "settings.enlarge_the_settings_window")} onClick={() => setSettingsWide(!wide)}>
          <StudioIcon name={wide ? "collapseAll" : "expand"} />
        </button>
        <button className="btn sm" data-action="settings.close" disabled={updatingPkg.applying} onClick={close}>
          {t("common.close")} <span className="esc">Esc</span>
        </button>
        {wide && <WindowControls />}
        </div>
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
      <ConfigTrouble port={port} onRepaired={onChanged} />
      <div className="prefs-body">
        {!solo && <SettingsNav at={at} values={nav} shown={shown} danger={danger} disabled={updatingPkg.applying} onSelect={(section) => { if (section !== at || deepAt) navigation.leave(() => { setLanded(""); setDeep(null); setAt(section); }); }} page={pageNow} onPage={(section, page) => navigation.leave(() => { setLanded(""); setPages((p) => ({ ...p, [section]: page })); })} />}

        <div className="prefs-main" key={at} role="tabpanel" aria-labelledby={pageNow ? `prefs-${at}-${pageNow}` : `prefs-${at}`} data-sec={at}>
          {!solo && <SettingsTitle at={at} />}
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
          <SettingDeepContext.Provider value={{ pages: true, open: deepAt, section: t(SECTION_NAME[at] ?? at), go: (id) => navigation.leave(() => { setLanded(""); setDeep(id); }) }}>
          <div className="prefs-col" key={`${at}:${deepAt ?? ""}`} data-cols={TWO_COLUMN.includes(at) && !deepAt && !solo ? 2 : undefined} data-deep={deepAt ?? undefined}>
          <SettingsPageStyle section={at} page={pageNow} />
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
          {at === "session" && (
            <>
              <SettingDeep id="agent-presets" title={t("persona.agent_presets")} hint={t("persona.presets_decide_what_new_tasks")}>
                <AgentPresets port={port} focusPersona={landed === "persona"} />
              </SettingDeep>
              <SectionCard title={t("settings.plans_and_questions")} hint={t("settings.how_plans_are_approved_how")}>
              {/* An on/off that reverses by clicking again is a switch, not two
                  options — the same control the recipes and the sandbox's
                  network egress use. */}
              <Group id="plan-mode" title={t("settings.plan_mode")} hint={t("settings.while_this_is_on_the")}>
                <div className="lrow">
                  <span className="tx">
                    <span className="lb">{t("settings.drafts_a_plan_read_only")}</span>
                    <span className="ds">
                      {t(status?.plan ? "settings.reads_and_drafts_a_plan" : "settings.normal_execution_approved_writes_are")}
                    </span>
                  </span>
                  <Switch
                    data-action="plan.mode"
                    on={status?.plan === true}
                    busy={busy === "plan"}
                    label={t("settings.plan_mode")}
                    onClick={() => run("plan", () => port.setPlanMode(!status?.plan))}
                  />
                </div>
              </Group>
              <Group id="plan-approval"
                title={t("settings.plan_approval")}
                hint={t("settings.after_plan_mode_submits_a")}
              >
                <PlanApproval port={port} />
              </Group>
                <DisplaySettings port={port} />
                <AskTimeout port={port} />
              <Group id="preset" title={t("settings.completion_standard")} now={preset} hint={t("settings.reasonix_chooses_the_needed_verification")}>
                <p className="note">{t("settings.everyday_questions_stay_lightweight_file")}</p>
              </Group>
                <ProgressWatch port={port} />
              </SectionCard>
              <SectionCard title={t("settings.collaboration")} hint={t("settings.quoting_past_sessions_and_messages")}>
                <SessionRef port={port} />
                <PeersSection port={port} hub={hub} />
              </SectionCard>
            </>
          )}

          {at === "model" && (
            <>
              <Group id="model" title={t("settings.models_by_use")} now={nav.model}
                hint={t("settings.choose_a_model_and_reasoning")}>
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
              <Group id="context" title={t("settings.context_maintenance")}
                hint={t("settings.compacts_automatically_from_model_capacity")}>
                <Compaction port={port} />
              </Group>
            <ProviderHealthSetting port={port} onChanged={onChanged} onFailed={setFailed} />
            <Group id="providers" title={t("common.model_services")}
              hint={t("settings.pick_a_service_to_see")}>
              <Providers port={port} onChanged={() => { loadModels(); onChanged(); }} onFailed={setFailed} protocol={protocol}
                activeKindFor={(a) => kindFor(a.key)}
                declare={declare}
                onProtocol={(a, kind) => switchProtocol(a.key, kind)} />
            </Group>
            </>
          )}

          {at === "tools" && (
            <>
              <SectionCard title={t("settings.approval")} hint={t("settings.when_it_stops_to_ask")}>
              <Group id="approval" title={t("settings.tool_approvals")} now={approval} hint={t("settings.this_is_the_only_approval")}>
                {/* Four rows of label-and-description was 190px for one choice,
                    and it was the only choice in this pane shaped that way. The
                    description follows the selection instead: what a档 does is
                    read when it is picked, not compared four at a time. */}
                <div className="seg" data-text data-danger={status?.toolApprovalMode === "yolo" ? "" : undefined}
                  role="radiogroup" aria-label={t("settings.tool_approvals")}>
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
              <Group id="external-origin"
                title={t("settings.outside_text")}
                hint={t("settings.text_in_web_pages_and")}
              >
                <ExternalOrigin port={port} onChanged={onChanged} />
              </Group>
              <Group id="rules"
                title={t("settings.rules_you_spell_out")}
                hint={t("settings.the_setting_above_decides_whether")}
              >
                <Rules port={port} onChanged={onChanged} />
              </Group><ToolList port={port} />
              </SectionCard>
              <SectionCard title={t("settings.sandbox_and_execution")} hint={t("settings.where_its_commands_can_write")}>
              <Group id="sandbox"
                title={t("settings.sandbox")}
                hint={t("settings.how_far_an_approved_action")}
              >
                <Sandbox port={port} onChanged={onChanged} />
              </Group>
              <Group id="shell"
                title={t("common.which_program_runs_commands")}
                hint={t("settings.every_command_the_agent_runs")}
              >
                <ShellPicker port={port} onChanged={onChanged} />
              </Group>
              </SectionCard>
              <SectionCard title={t("settings.web_and_browser")} hint={t("settings.search_page_fetching_and_the")}>
              <Group id="browser-tools"
                title={t("common.built_in_browser")}
                hint={t("settings.the_tools_the_agent_uses")}
              >
                <BrowserTools port={port} onChanged={onChanged} />
              </Group>
              <WebAccess port={port} />
              <SessionCoordination port={port} onChanged={onChanged} />
              </SectionCard>
            </>
          )}

          {at === "display" && (
            <>
              <Group id="present-files"
                title={t("settings.ai_file_delivery_cards")}
                hint={t("settings.file_previews_remain_available_from")}
              >
                <PresentFiles port={port} />
              </Group>
            </>
          )}

          {at === "ext" && (
            <>
              <div className="ext-bar">
                <ExtTabs at={extTab} disabled={updatingPkg.applying} onPick={(tab) => { setInstalledTarget(null); setExtTab(tab); }} />
                <ExtAddMenu disabled={updatingPkg.applying} offer={new Set<ExtAddWay>(["market", ...(updatingPkg.name ? [] : ["source", "folder"] as const), ...(live ? ["service"] as const : [])])}
                  onPick={(way) => { setInstalledTarget(null); setExtTab(way === "market" ? "market" : "installed"); if (way === "service") setAdding(true); else if (way !== "market") setAddingPkg(way); }} />
              </div>
              {extTab === "installed" && <InstalledLocation root={root} target={installedTarget} packages={packages} skills={skills} mcp={mcp} refreshing={extRefreshing} onFound={setInstalledTarget} />}
              {extTab === "market" && <MarketGroup port={port} onApplying={applyingChanged} onInstalled={afterExtChange} onViewInstalled={(kind, name) => { setInstalledTarget({ kind, name }); setExtTab("installed"); }} account={acct} onSignIn={() => go("account", "account")} />}
              {extTab === "installed" && (
              <>
              <ExtSplit on={solo} picked={picked ?? firstPick(packages, looseMcp, looseSkills)} onPick={setPicked} detail={
                <ExtDetail picked={picked ?? firstPick(packages, looseMcp, looseSkills)} packages={packages} mcp={mcp} skills={skills} port={port} root={scopeAt} live={live} implicit={implicit}
                  updating={updatingPkg.name} onChanged={afterExtChange} onReloadError={reload.report} onReloaded={reload.applied}
                  onUpdate={(name) => setUpdatingPkg((p) => ({ ...p, name, applying: false }))} onFailed={setFailed} />
              }>
                {scope && <ScopeBar scope={scope} scopes={scopes} onPick={setScopeAt} />}
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
                  title={t("settings.external_tools")}
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
                <Group id="ext-runtime"
                  title={t("settings.runtime")}
                  hint={t("settings.changes_apply_from_the_next_turn")}
                  action={reload.action}
                >
                  {reload.note}
                </Group>
              </ExtSplit>
              </>
              )}
            </>
          )}
          {at === "ext" && extTab === "installed" && (
            <>
              <Group id="hooks"
                title={t("settings.automation")}
                hint={t("settings.run_your_own_commands_before")}
              >
                <Hooks port={port} onChanged={afterExtChange} />
              </Group>
              <ScheduledRuns port={port} />
            </>
          )}
          {at === "network" && (
            <Group id="network"
              title={t("common.network")}
              hint={t("settings.model_requests_remote_mcp_services")}
            >
              <Network port={networkPort ?? port} host={networkHost ?? ""} />
            </Group>
          )}
          {at === "account" && <TraceExport port={port} />}

          {at === "network" && shown("remote") && (
            <Group id="remote"
              title={t("settings.remote")}
              hint={t("settings.connect_a_workspace_on_another")}
            >
              <Remotes hub={hub} onError={onError} />
            </Group>
          )}
          {at === "network" && shown("remote") && <PhoneAccess hub={hub} onError={onError} />}
          {at === "network" && shown("remote") && <PhoneNotifications hub={hub} port={port} onError={onError} />}
          {at === "network" && shown("remote") && remoteDeviceId() && <PhoneReminders host={remoteDeviceId()!} />}

          {at === "account" && (
            <Group id="account"
              title={t("settings.account")}
              hint={t("settings.reasonix_itself_needs_no_account")}
            >
              <Account port={port} state={acct} unread={accountUnread} reload={reloadAccount} />
            </Group>
          )}
          {at === "account" && <About port={homePort} />}
          {at === "storage" && (
            <DataSettings port={port} homePort={homePort} localPort={localPort} hub={hub} workspace={workspaceRoot ?? (homePort === port ? status?.workspaceRoot ?? "" : "")} cwd={status?.cwd} busy={busy}
              signedIn={!!acct?.signedIn && !!acct.user} onIsolate={() => run("isolate", () => port.isolateWorkspace())} onChanged={onChanged} onRecovered={() => { onChanged(); onSessionsRecovered?.(); }} />
          )}

          {at === "common" && <CommonSettings port={port} homePort={homePort} theme={theme} onTheme={onTheme} look={look} onLook={onLook} />}

          {LOOK_FAMILY.includes(at) && (
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
          </SettingDeepContext.Provider>
        </Boundary>
        </div>
      </div>
      </div>
    </div></SettingsLeaveContext.Provider>
  );
}
