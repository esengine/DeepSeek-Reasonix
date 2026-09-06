import { Fragment, useCallback, useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { useRuntimeReload } from "./RuntimeReload";
import type { AccountState, AgentPort, Appearance as Look, ApprovalMode, CapabilityScope, McpEntry, ModelEntry, PluginPackage, Preset, RoleAssignments, SessionStatus, SkillEntry } from "../port/port";
import { arrowTabs } from "./tablist";
import { bytes, tokens as fmtTokens } from "../i18n/format";
import { ICON, NAV } from "./prefsnav";
import type { Section } from "./prefsnav";
import { AddServer } from "./AddServer";
import { Remotes } from "./Remotes";
import type { HubPort } from "../port/hub";
import type { RemoteHost } from "../port/remote";
import { AddPlugin } from "./AddPlugin";
import { Packages } from "./Packages";
import { Switch } from "./Switch";
import { ServerRow } from "./ServerRow";
import { SkillRow } from "./SkillRow";
import { Hooks } from "./Hooks";
import { Network } from "./Network";
import { Shell as ShellPicker } from "./Shell";
import { Rules } from "./Rules";
import { ConfigTrouble } from "./ConfigTrouble";
import { Compaction } from "./Compaction";
import { Sandbox } from "./Sandbox";
import { Account } from "./Account";
import { Providers } from "./Providers";
import { Models, activeKind, groupVendors } from "./Models";
import { KIND_LABEL } from "./vendors";
import { planProtocolSwitch } from "./protocolswitch";
import { Roles } from "./Roles";
import { Boundary } from "./Boundary";
import { Versions } from "./Versions";
import { Memory } from "./Memory";
import { DEFAULT_DAYS, Usage } from "./Usage";
import { Storage } from "./Storage";
import { Appearance, SCHEMES } from "./Appearance";
import { ScopeBar } from "./CapabilityScope";
import { reason } from "../i18n/kernel";

const PRESETS: [Preset, string, string][] = [
  ["balanced", "均衡", "做到模型认为做完为止。日常用这档"],
  ["delivery", "交付", "改了东西就得验证、复核、签收，少一样都不算做完"],
];

const APPROVALS: [ApprovalMode, string, string][] = [
  ["dontAsk", "不打扰", "不弹审批；要批准才能做的一概不做"],
  ["ask", "询问", "每次动手前问你"],
  ["auto", "自动", "低风险自己过，写操作仍然问"],
  ["yolo", "全放行", "不问了。只在你完全信任这个工作区时用"],
];

// What still lives in the old desktop app. Bots are not on the roadmap, so
// they are not a promise to keep here either. Signing in and reading
// versions landed here; downloading and applying an update did not, and naming
// only that half keeps this list a fact rather than a promise.
// Everything that used to live here has a home now, so the 「高级」 tab filters
// itself out below. Keep the list rather than the tab: the next thing that is
// real but not built yet belongs in one line here, not in a half-made panel.
const ELSEWHERE: string[] = [];

// The user's question is "is it there and does it work", so the state is the
// label. A failed server keeps its error on the row that names it.
const NET_MODE: Record<string, string> = { auto: "跟随系统", env: "环境变量", custom: "手动", off: "直连" };

interface Props {
  // The window's own port, not a pane's: remote machines belong to the window,
  // and their endpoints sit above every pane's prefix.
  hub: HubPort;
  onError: (e: unknown) => void;
  port: AgentPort;
  status: SessionStatus | null;
  theme: string;
  reloadThemes: () => void;
  onTheme: (t: string) => void;
  contrast: string;
  weight: string;
  onWeight: (v: string) => void;
  look: Look;
  onLook: (look: Look) => void;
  onContrast: (c: string) => void;
  onClose: () => void;
  onChanged: () => void;
  at?: string;
  account: AccountState | null;
  reloadAccount: () => void;
}

export function Settings({ hub, onError, port, status, theme, onTheme, contrast, onContrast, weight, onWeight, look, onLook, onClose, onChanged, reloadThemes, at: opened, account: acct, reloadAccount }: Props) {
  const [at, setAt] = useState<Section>((opened as Section) || "session");
  const [models, setModels] = useState<ModelEntry[]>([]);
  const [roles, setRoles] = useState<RoleAssignments | null>(null);
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
  const [failed, setFailed] = useState("");
  const [adding, setAdding] = useState(false);
  const [remoteBook, setRemoteBook] = useState<RemoteHost[] | null>(null);
  const [packages, setPackages] = useState<PluginPackage[]>([]);
  const [addingPkg, setAddingPkg] = useState(false);
  const [updatingPkg, setUpdatingPkg] = useState("");
  const [hookCount, setHookCount] = useState(0);
  const [netMode, setNetMode] = useState("");
  const [memCount, setMemCount] = useState(0);
  const [ruleCount, setRuleCount] = useState(0);
  const root = useRef<HTMLDivElement>(null);
  const veiled = useRef(false);

  const reloadExt = useCallback(() => {
    const where = scopeAt || undefined;
    port.capabilityScopes().then(setScopes).catch(() => setScopes([]));
    port
      .mcp(where)
      .then((c) => {
        setMcp(c.servers);
        setScope(c.scope);
        setLive(c.live !== false);
      })
      .catch(() => setMcp([]));
    port.plugins().then(setPackages).catch(() => setPackages([]));
    port.hooks().then((c) => setHookCount(c.hooks.length)).catch(() => setHookCount(0));
    port.network().then((n) => setNetMode(t(NET_MODE[n.mode] ?? n.mode))).catch(() => setNetMode(""));
    port.memories().then((c) => setMemCount(c.memories.length)).catch(() => setMemCount(0));
    port
      .permissions()
      .then((p) => setRuleCount(p.deny.length + p.ask.length + p.allow.length))
      .catch(() => setRuleCount(0));
    port
      .skills(where)
      .then((c) => {
        setSkills(c.skills);
        setImplicit(c.implicit);
      })
      .catch(() => setSkills([]));
  }, [port, scopeAt]);

  // An extension switch moves the metrics rail too, so the change has to leave
  // this pane as well as refresh it.
  const afterExtChange = useCallback(() => {
    reloadExt();
    onChanged();
  }, [reloadExt, onChanged]);

  const reload = useRuntimeReload(port, afterExtChange);

  // Adding or removing a source changes what the picker above can offer, so
  // the list is reloadable rather than read once at mount.
  const loadModels = useCallback(() => {
    port.models().then(setModels).catch(() => setModels([]));
  }, [port]);

  const loadRoles = useCallback(() => {
    port.roles().then(setRoles).catch(() => setRoles(null));
  }, [port]);

  // Three sections whose row used to report nothing. Loaded here rather than in
  // reloadExt because none of them moves with the scope the extension lists are
  // read at, and dropped on failure: a table of contents that says "—" where a
  // number failed to arrive has invented a fact about the section.
  const [tally, setTally] = useState<Partial<Record<Section, string>>>({});
  useEffect(() => {
    const put = (k: Section, v: string) => setTally((prev) => ({ ...prev, [k]: v }));
    port.storage().then((st) => put("storage", bytes(st.roots.reduce((n, r) => n + (r.bytes || 0), 0)))).catch(() => {});
    // The same window Usage itself opens on, or the list and the page it opens
    // would report two different totals for one word.
    port.usage(DEFAULT_DAYS).then((u) => put("usage", fmtTokens(u.tokens))).catch(() => {});
    port.versions().then((v) => put("versions", v.current || "dev")).catch(() => {});
  }, [port]);

  // Focus lands once, when the pane opens. onClose is a fresh arrow on every
  // parent render, so re-running this with it would pull focus back out of
  // whatever the user is holding — a native select closes its dropdown the
  // instant it is blurred, which reads as the menu refusing to open at all.
  useEffect(() => {
    root.current?.focus();
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, [onClose]);

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
    setFailed("");
    try {
      await fn();
      onChanged();
    } catch (e) {
      setFailed(reason(e));
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
      setFailed(t("{door} 不提供 {model}，请在下方选择该来源支持的模型。", {
        door: t(KIND_LABEL[kind] ?? kind),
        model: plan.model,
      }));
      return;
    }
    setProtocol((p) => ({ ...p, [key]: kind }));
    if (plan.do === "switch") run(plan.ref, () => port.setModel(plan.ref));
  };
  const efforts = models.find((m) => m.ref === status?.modelRef)?.efforts ?? [];
  const assigned = roles ? Object.values(roles).filter(Boolean).length : 0;
  const preset = t(PRESETS.find(([id]) => id === status?.preset)?.[1] ?? "") || "—";
  const approval = t(APPROVALS.find(([id]) => id === status?.toolApprovalMode)?.[1] ?? "—");
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
  const remoteTally = remoteBook === null ? "" : online ? t("{n} 台在线", { n: online }) : remoteBook.length ? `${remoteBook.length}` : "";
  // The table of contents is also the status board: the value that matters for
  // each section rides on its own row, so the risky one is legible from here.
  const nav: Record<Section, string> = {
    session: status?.plan ? t("计划模式") : preset,
    model: status?.modelRef?.split("/").pop() ?? "—",
    tools: ruleCount ? `${approval} · ${ruleCount}` : approval,
    hooks: hookCount ? t("{n} 条", { n: hookCount }) : t("关"),
    ext: broken ? t("{n} 个异常", { n: broken }) : packages.length ? t("{n} 个包", { n: packages.length }) : `${looseMcp.length + looseOn}`,
    network: netMode,
    remote: remoteTally,
    memory: memCount ? t("{n} 条", { n: memCount }) : "",
    usage: tally.usage ?? "",
    storage: tally.storage ?? "",
    account: acct === null ? "" : acct.signedIn ? (acct.user?.label ?? t("已登录")) : t("未登录"),
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

  return (
    <div
      className="prefs"
      ref={root}
      tabIndex={-1}
      onMouseDown={(e) => { veiled.current = e.target === e.currentTarget; }}
      // Both ends of the press have to land on the veil. A drag that started
      // inside the sheet and let go outside is a selection, not a dismissal.
      onMouseUp={(e) => { if (veiled.current && e.target === e.currentTarget) onClose(); }}
    >
      <div className="prefs-sheet" role="dialog" aria-modal="true" aria-label={t("设置")}>
      <div className="prefs-hd">
        <h2>{t("设置")}</h2>
        <p>{t("改动立刻生效；需要重建运行时的项目，在任务运行期间无法修改")}</p>
        <button className="btn sm" onClick={onClose}>
          {t("关闭")} <span className="esc">Esc</span>
        </button>
      </div>

      <ConfigTrouble port={port} onRepaired={onChanged} />

      <div className="prefs-body">
        <nav className="prefs-nav" role="tablist" aria-label={t("设置分类")} onKeyDown={arrowTabs}>
          {NAV.map(([group, items]) => {
            const rows = items.filter(([id]) => shown(id));
            // A heading over nothing is worse than no heading: remote is absent
            // on a kernel without it, advanced until something moves in.
            if (rows.length === 0) return null;
            return (
              <Fragment key={group}>
                <div className="navsec">{t(group)}</div>
                {rows.map(([id, name]) => (
                  <button key={id} id={`prefs-${id}`} role="tab" title={t(name)}
                    aria-selected={at === id} onClick={() => setAt(id)}>
                    <svg viewBox="0 0 16 16" aria-hidden="true">{ICON[id]}</svg>
                    <span className="nm">{t(name)}</span>
                    <span className="nv" title={nav[id] || undefined} data-danger={danger(id) ? "" : undefined}>
                      {nav[id]}
                    </span>
                  </button>
                ))}
              </Fragment>
            );
          })}
        </nav>

        <div className="prefs-main" role="tabpanel" aria-labelledby={`prefs-${at}`} data-sec={at}>
          <Boundary
            fallback={
              <div className="find" data-lvl="err" role="alert">
                <span className="t">{t("这个设置分区出错了")}</span>
                <span className="why">{t("其它分区和你的会话不受影响；关掉设置再打开可重试。")}</span>
              </div>
            }
          >
          <div className="prefs-col">
          {at === "session" && (
            <>
              <Group title={t("执行设定")} now={preset} hint={t("决定任务完成的判定标准。切换立刻生效，不会重建运行时。")}>
                <div className="seg" data-text role="radiogroup" aria-label={t("执行设定")}>
                  {PRESETS.map(([id, name]) => (
                    <button key={id} role="radio" aria-checked={status?.preset === id} disabled={!!busy}
                      onClick={() => run(id, () => port.setPreset(id))}>
                      {t(name)}
                    </button>
                  ))}
                </div>
                <p className="note">{t(PRESETS.find(([id]) => id === status?.preset)?.[2] ?? "")}</p>
              </Group>
              {/* An on/off that reverses by clicking again is a switch, not two
                  options — the same control the recipes and the sandbox's
                  network egress use. */}
              <Group title={t("计划模式")} hint={t("开启时 agent 无法获得写权限。该限制由工具本身实施，不依赖提示词中的约定。")}>
                <div className="lrow">
                  <span className="tx">
                    <span className="lb">{t("只读地出计划")}</span>
                    <span className="ds">
                      {t(status?.plan ? "只读加出计划，你批准后核心自己关掉它" : "正常执行：批准过的写操作直接做")}
                    </span>
                  </span>
                  <Switch
                    on={status?.plan === true}
                    busy={busy === "plan"}
                    label={t("计划模式")}
                    onClick={() => run("plan", () => port.setPlanMode(!status?.plan))}
                  />
                </div>
              </Group>
              <Group title={t("这个会话在哪写")}>
                <div className="kv">
                  <span className="k">{t("工作目录")}</span>
                  <span className="v">{status?.cwd ?? "—"}</span>
                </div>
                <p className="note">
                  {t("在左栏管理文件夹：底部添加，展开后可新建会话。会话归属于创建它的文件夹，不会移动到其他位置。")}
                </p>
                {/* A thing that happens once, not a state to sit in — so it is
                    a button. As an option row it carried a selected look it can
                    never have. */}
                <div className="lrow">
                  <span className="tx">
                    <span className="lb">{t("拉一份隔离副本")}</span>
                    <span className="ds">{t("在 Git worktree 中创建独立副本，改动不会影响当前分支")}</span>
                  </span>
                  <button className="act" disabled={!!busy} onClick={() => run("isolate", () => port.isolateWorkspace())}>
                    {t(busy === "isolate" ? "开着…" : "开一份")}
                  </button>
                </div>
              </Group>
            </>
          )}

          {at === "model" && (
            <>
              {/* Every switch on this page goes through run(), so one place to
                  say why one was refused covers all of them. */}
              {failed && (
                <div className="find" data-lvl="warn" role="alert">
                  <span className="t">{t("这一步没做成")}</span>
                  <span className="why">{failed}</span>
                </div>
              )}
              <Group title={t("分工")} now={roles ? t("{n} 个已指派", { n: assigned }) : undefined}
                hint={t("每个位置默认使用主模型，只有明确指派过的才会单独设置。更换指派与更换主模型一样需要重建运行时，任务运行期间无法修改。")}>
                <Roles models={models} roles={roles} main={status?.modelRef} busy={busy}
                  onSet={(role, ref) => run(`role:${role}`, async () => {
                    await port.setRole(role, ref);
                    loadRoles();
                  })} />
              </Group>
              <Group title={t("模型")} now={nav.model} hint={t("切换会保留对话并重建运行时，任务运行期间无法切换。标签只显示已探测到的能力；留空表示端点未声明，不代表不支持。")}>
                <Models models={models} current={status?.modelRef} busy={busy} protocol={protocol}
                  onPick={(ref) => run(ref, () => port.setModel(ref))} />
              </Group>
              {efforts.length > 0 ? (
                <Group title={t("推理强度")} hint={t("以下档位由当前模型的端点支持，auto 表示使用端点自身的默认值。")}>
                  <div className="seg" role="group" aria-label={t("推理强度")}>
                    {efforts.map((e) => (
                      <button key={e} aria-pressed={(status?.effort || "auto") === e}
                        onClick={() => run(e, () => port.setEffort(e))}>
                        {e}
                      </button>
                    ))}
                  </div>
                </Group>
              ) : (
                <Group title={t("推理强度")} hint={t("当前模型未提供可调的推理档位，因此不显示该选项。")} />
              )}
              <Group title={t("上下文维护")}
                hint={t("对话长到一定程度会折叠成摘要再继续。折叠点取「窗口比例」与「压缩阈值」中先到的那个。")}>
                <Compaction port={port} onChanged={onChanged} />
              </Group>
              <Group
                title={t("连接")}
                hint={t("模型的来源。添加时只需填写地址和 key；协议、模型列表和图片支持会自动向端点探测，探测不到的才需要手动填写。")}
              >
                <Providers port={port} onChanged={loadModels} onFailed={setFailed} protocol={protocol}
                  activeKindFor={(a) => kindFor(a.key)}
                  onProtocol={(a, kind) => switchProtocol(a.key, kind)} />
              </Group>
            </>
          )}

          {at === "tools" && (
            <>
              <Group title={t("工具批准")} now={approval} hint={t("这是 agent 访问你的文件前的唯一审批入口，被拦下的操作没有其他途径可以绕过。")}>
                {/* Four rows of label-and-description was 190px for one choice,
                    and it was the only choice in this pane shaped that way. The
                    description follows the selection instead: what a档 does is
                    read when it is picked, not compared four at a time. */}
                <div className="seg" data-text data-danger={status?.toolApprovalMode === "yolo" ? "" : undefined}
                  role="radiogroup" aria-label={t("工具批准")}>
                  {APPROVALS.map(([id, name]) => (
                    <button key={id} role="radio" aria-checked={status?.toolApprovalMode === id} disabled={!!busy}
                      onClick={() => run(id, () => port.setApprovalMode(id))}>
                      {t(name)}
                    </button>
                  ))}
                </div>
                <p className="note">{t(APPROVALS.find(([id]) => id === status?.toolApprovalMode)?.[2] ?? "")}</p>
              </Group>
              <Group
                title={t("明确的规矩")}
                hint={t("上一项决定是否向你询问，此处决定哪些操作始终禁止、哪些无需询问。改动会重建运行时，任务运行期间无法修改。")}
              >
                <Rules port={port} onChanged={onChanged} />
              </Group>
              <Group
                title={t("沙箱")}
                hint={t("批准之后可以操作的范围。该限制不依赖 agent 自觉：写入范围由工具实施，命令隔离由操作系统实施。")}
              >
                <Sandbox port={port} onChanged={onChanged} />
              </Group>
              <Group
                title={t("命令交给谁执行")}
                hint={t("agent 的所有命令都由该程序执行，它也决定命令使用哪种语法；选择错误会导致每条命令都执行失败。下方只列出本机已安装的程序。更换需要重建运行时，任务运行期间无法修改。")}
              >
                <ShellPicker port={port} onChanged={onChanged} />
              </Group>
            </>
          )}

          {at === "hooks" && (
            <Group
              title={t("自动化")}
              hint={t("在 agent 执行任务前后运行你自己的命令。这些命令在本机以你的权限运行；可以拦截 agent 的两个事件已在下方标出。")}
            >
              <Hooks port={port} onChanged={afterExtChange} />
            </Group>
          )}

          {at === "ext" && (
            <>
              {scope && <ScopeBar scope={scope} scopes={scopes} onPick={setScopeAt} />}
              {failed && (
                <div className="find" data-lvl="warn" role="alert">
                  <span className="t">{t("这一步没做成")}</span>
                  <span className="why">{failed}</span>
                </div>
              )}
              {/* 装完、改完、删完都要过这一步才算数——把它放在包列表上面，
                  因为它管的是整个运行时，不是某一个包。 */}
              <Group
                title={t("运行时")}
                hint={t("修改扩展代码，或安装、删除、启用或停用插件包之后，用它使改动生效。当前这一轮不受影响，下一轮开始使用新的配置。")}
                action={reload.action}
              >
                {reload.note}
              </Group>
              <Group
                title={t("插件包")}
                now={packages.length ? t("{n} 个", { n: packages.length }) : undefined}
                hint={t("一个包可以同时提供技能、命令、自动化钩子和外部服务。安装与导入是同一个操作：提供一个仓库地址，或本机的一个文件夹。")}
                action={
                  addingPkg ? undefined : (
                    <button className="act" onClick={() => setAddingPkg(true)}>
                      {t("添加")}
                    </button>
                  )
                }
              >
                {addingPkg && (
                  <AddPlugin port={port} onClose={() => setAddingPkg(false)} onInstalled={afterExtChange} />
                )}
                {updatingPkg && (
                  <AddPlugin
                    port={port}
                    updating={packages.find((p) => p.name === updatingPkg)}
                    onClose={() => setUpdatingPkg("")}
                    onInstalled={afterExtChange}
                  />
                )}
                <Packages
                  port={port}
                  packages={packages}
                  onChanged={afterExtChange}
                  updating={updatingPkg}
                  onUpdate={setUpdatingPkg}
                />
                {packages.length === 0 && !addingPkg && <div className="empty">{t("还没装插件包。")}</div>}
              </Group>
              {/* Below the packages: what was added by hand. A server the user
                  typed in themselves is not part of anyone's package, and
                  filing it under one would misname where it came from. */}
              <Group
                title={t("外部工具")}
                now={looseMcp.length ? t("{n} 个服务", { n: looseMcp.length }) : undefined}
                hint={t("你自行接入的 MCP 服务。它们提供的能力与内置工具等同，列出的每一项都可以操作你的文件和数据。关闭后会立即从本轮工具列表中移除，重启后保持关闭。")}
                action={
                  adding || !live ? undefined : (
                    <button className="act" onClick={() => setAdding(true)}>
                      {t("接入服务")}
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
                {looseMcp.length === 0 && !adding && <div className="empty">{t("没有自己接入的外部服务。")}</div>}
              </Group>
              <Group
                title={t("技能")}
                now={looseSkills.length ? t("{on}/{all} 开着", { on: looseOn, all: looseSkills.length }) : undefined}
                hint={t(
                  implicit
                    ? "工作目录与「我的」中的技能。带 / 的可以由你直接调用，其余的由模型根据任务判断是否使用。关掉一个立刻生效：从你的下一条消息起模型不再拿到它，直接点名也调不动，不用新建会话。"
                    : "模型自动发现已关闭：现在只有你点名的技能会跑。开关立刻生效，从你的下一条消息起，不用新建会话。",
                )}
              >
                {looseSkills.map((sk) => (
                  <SkillRow key={sk.name} sk={sk} implicit={implicit} port={port} onDone={afterExtChange} root={scopeAt} onFailed={setFailed} />
                ))}
                {looseSkills.length === 0 && <div className="empty">{t("这个工作目录下没有技能。")}</div>}
              </Group>
            </>
          )}

          {at === "network" && (
            <Group
              title={t("网络")}
              hint={t("模型请求、MCP 远程服务和网页抓取都经由此处。配置错误通常表现为聊天无响应，建议先测试连接，测试会指出中断的环节。")}
            >
              <Network port={port} />
            </Group>
          )}

          {at === "remote" && (
            <Group
              title={t("远程")}
              hint={t("接入另一台机器上的工作区：内核在远程运行，界面仍在本机。远程面板与本地面板并排显示，每处都会标明所在的机器。")}
            >
              <Remotes hub={hub} onError={onError} />
            </Group>
          )}

          {at === "account" && (
            <Group
              title={t("账号")}
              hint={t("Reasonix 本身不需要账号，仅在需要联网的功能中使用：社区发帖、崩溃问题跟进，以及后续的技能发布。")}
            >
              <Account port={port} state={acct} reload={reloadAccount} />
            </Group>
          )}

          {at === "versions" && (
            <Group
              title={t("版本")}
              hint={t("当前安装的版本、可用更新，以及出现问题时如何回退。回退后将固定在你选择的版本，不会被自动更新覆盖。")}
            >
              <Versions port={port} />
            </Group>
          )}

          {at === "memory" && (
            <Group
              title={t("记忆")}
              hint={t("agent 自动记录的内容：你没有配置过，但它会据此执行。此处按触发时机分组，并标出上一轮实际使用的条目。")}
            >
              <Memory port={port} />
            </Group>
          )}

          {at === "usage" && (
            <Group
              title={t("用量与成本")}
              hint={t("本机记录的 token 用量与花费，仅保存在这台机器上，不会上传。命中缓存的输入按缓存价计费，因此命中率直接影响费用。")}
            >
              <Usage port={port} />
            </Group>
          )}

          {at === "storage" && (
            <Group title={t("存储")} hint={t("数据的存储位置与占用空间。会话和索引会持续增长，配置和凭据不会，因此只有前者可以迁移。迁移在重启后生效。")}><Storage port={port} /></Group>
          )}

          {at === "appearance" && (
            <Appearance port={port} theme={theme} onTheme={onTheme} contrast={contrast} onContrast={onContrast} weight={weight} onWeight={onWeight} reloadThemes={reloadThemes} look={look} onLook={onLook} />
          )}

          {at === "advanced" && (
            <Group title={t("还不在这一版里")} hint={t("以下项目尚未提供设置界面，此处仅说明它们当前的位置。")}>
              {ELSEWHERE.map((x) => (
                <div className="lrow" key={x}>
                  <span className="ds">{x}</span>
                  <span className="sc">{t("旧版桌面端")}</span>
                </div>
              ))}
            </Group>
          )}
          </div>
        </Boundary>
        </div>
      </div>
      </div>
    </div>
  );
}

function Group({
  title, hint, now, action, children,
}: {
  title: string; hint?: string; now?: string; action?: React.ReactNode; children?: React.ReactNode;
}) {
  return (
    <section className="grp">
      <div className="grp-hd">
        <h2>{title}</h2>
        {now && <span className="now">{now}</span>}
        {action}
      </div>
      {hint && <p className="hint">{hint}</p>}
      {children && <div className="grp-items">{children}</div>}
    </section>
  );
}

