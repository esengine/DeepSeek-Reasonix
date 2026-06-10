import { memo, useCallback, useDeferredValue, useEffect, useMemo, useRef, useState, type CSSProperties, type WheelEvent } from "react";
import type { LucideIcon } from "lucide-react";
import {
  AlertTriangle,
  ArrowRight,
  Bot,
  Check,
  ChevronDown,
  Database,
  FileText,
  FolderOpen,
  Globe2,
  LoaderCircle,
  Mail,
  MoreHorizontal,
  Palette,
  Plus,
  Plug,
  Presentation,
  Puzzle,
  RefreshCw,
  Search,
  Settings,
  Sparkles,
  Table2,
  Wrench,
  X,
} from "lucide-react";
import { app } from "../lib/bridge";
import { useI18n } from "../lib/i18n";
import type { CapabilitiesView, ServerView, SkillView } from "../lib/types";
import { MCPServersSettingsPage, SkillsSettingsPage } from "./CapabilitiesPanel";
import { VirtualList } from "./VirtualList";

type CatalogMode = "plugins" | "skills";
type CatalogFilter = "all" | "builtIn" | "enabled";
type CatalogSource = "server" | "skill";
type StatusTone = "ok" | "warn" | "info" | "off";

type CatalogItem = {
  id: string;
  name: string;
  description: string;
  meta: string;
  status: string;
  statusTone: StatusTone;
  enabled: boolean;
  builtIn: boolean;
  color: string;
  icon: LucideIcon;
  prompt: string;
  actionLabel: string;
  kind: CatalogMode;
  source: CatalogSource;
  removable: boolean;
};

function copy(locale: string) {
  const zh = locale === "zh";
  return {
    plugins: zh ? "插件" : "Plugins",
    skills: zh ? "技能" : "Skills",
    manage: zh ? "管理" : "Manage",
    create: zh ? "创建" : "Create",
    addPlugin: zh ? "添加插件" : "Add plugin",
    addSkill: zh ? "添加技能" : "Add skill",
    managerTitlePlugins: zh ? "插件管理" : "Plugin management",
    managerTitleSkills: zh ? "技能管理" : "Skill management",
    managerDescPlugins: zh ? "添加、启用、重连和授权 MCP 插件。" : "Add, enable, reconnect, and authorize MCP plugins.",
    managerDescSkills: zh ? "添加技能来源，刷新发现结果，并启用或停用技能。" : "Add skill sources, refresh discovery, and enable or disable skills.",
    closeManager: zh ? "关闭管理" : "Close management",
    refresh: zh ? "刷新" : "Refresh",
    headline: zh ? "让 Reasonix 按你的方式工作" : "Make Reasonix work your way",
    search: zh ? "搜索插件、MCP 或技能" : "Search plugins, MCP, or skills",
    builtIn: zh ? "内置/本地" : "Built-in/local",
    all: zh ? "全部" : "All",
    enabled: zh ? "已启用" : "Enabled",
    live: zh ? "已绑定能力" : "Live bindings",
    featured: zh ? "精选能力" : "Featured capabilities",
    tryInChat: zh ? "在对话中试用" : "Try in chat",
    showFeatured: zh ? "切换精选能力" : "Show featured capability",
    noResults: zh ? "没有匹配的真实能力" : "No matching live capability",
    emptyTitle: zh ? "还没有可展示的插件或技能" : "No plugins or skills to show yet",
    emptyBody: zh ? "打开管理页添加 MCP 服务器，或在技能页刷新本地技能源。" : "Open Manage to add MCP servers, or refresh local skill sources.",
    loading: zh ? "正在读取真实能力..." : "Loading live capabilities...",
    loadFailed: zh ? "读取能力失败" : "Failed to load capabilities",
    retry: zh ? "重试" : "Retry",
    enable: zh ? "启用" : "Enable",
    disable: zh ? "禁用" : "Disable",
    removePlugin: zh ? "移除插件" : "Remove plugin",
    removeSkill: zh ? "删除技能" : "Remove skill",
    cannotRemove: zh ? "内置能力不可移除" : "Built-in capability cannot be removed",
    connected: zh ? "已连接" : "Connected",
    failed: zh ? "连接失败" : "Failed",
    initializing: zh ? "启动中" : "Starting",
    deferred: zh ? "按需启动" : "On demand",
    disabled: zh ? "已禁用" : "Disabled",
    skillEnabled: zh ? "已启用" : "Enabled",
    skillDisabled: zh ? "已禁用" : "Disabled",
    builtin: zh ? "内置" : "Built-in",
    local: zh ? "本地" : "Local",
    project: zh ? "项目" : "Project",
    custom: zh ? "自定义" : "Custom",
    mcp: "MCP",
    skill: zh ? "技能" : "Skill",
    tools: zh ? "工具" : "tools",
    prompts: zh ? "提示" : "prompts",
    resources: zh ? "资源" : "resources",
    noDescription: zh ? "来自当前会话的真实能力条目" : "Live capability from the current session",
    heroFallback: zh ? "从当前 Reasonix 会话读取 MCP 与技能状态" : "Read MCP and skill status from the current Reasonix session",
  };
}

function emptyCapabilities(): CapabilitiesView {
  return { servers: [], skills: [], skillRoots: [] };
}

function normalizeCapabilitiesView(view: CapabilitiesView | null | undefined): CapabilitiesView {
  return {
    servers: Array.isArray(view?.servers) ? view.servers : [],
    skills: Array.isArray(view?.skills) ? view.skills : [],
    skillRoots: Array.isArray(view?.skillRoots) ? view.skillRoots : [],
  };
}

export function PluginHub({
  onTry,
}: {
  onTry: (prompt: string) => void;
}) {
  const { locale } = useI18n();
  const c = copy(locale);
  const [mode, setMode] = useState<CatalogMode>("plugins");
  const [filter, setFilter] = useState<CatalogFilter>("all");
  const [query, setQuery] = useState("");
  const [view, setView] = useState<CapabilitiesView>(() => emptyCapabilities());
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [busyItem, setBusyItem] = useState<string | null>(null);
  const [heroIndex, setHeroIndex] = useState(0);
  const [managerMode, setManagerMode] = useState<CatalogMode | null>(null);
  const [addServerRequest, setAddServerRequest] = useState(0);
  const [addSkillFocusRequest, setAddSkillFocusRequest] = useState(0);
  const deferredQuery = useDeferredValue(query);
  const hubRef = useRef<HTMLElement | null>(null);
  const heroTrackRef = useRef<HTMLDivElement | null>(null);
  const heroScrollFrameRef = useRef<number | null>(null);

  const loadCapabilities = useCallback(async (silent = false) => {
    if (!silent) setLoading(true);
    setLoadError(null);
    try {
      const next = await app.Capabilities();
      setView(normalizeCapabilitiesView(next));
    } catch (error) {
      setView(emptyCapabilities());
      setLoadError(String((error as Error)?.message ?? error));
    } finally {
      if (!silent) setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadCapabilities();
  }, [loadCapabilities]);

  useEffect(() => {
    if (!view.servers.some((server) => server.status === "initializing" || server.status === "deferred")) return;
    const id = window.setInterval(() => void loadCapabilities(true), 2500);
    return () => window.clearInterval(id);
  }, [loadCapabilities, view.servers]);

  const items = useMemo(() => {
    const q = deferredQuery.trim().toLowerCase();
    return buildCatalogItems(view, c)
      .filter((item) => item.kind === mode)
      .filter((item) => {
        if (filter === "builtIn" && !item.builtIn) return false;
        if (filter === "enabled" && !item.enabled) return false;
        return true;
      })
      .filter((item) => {
        if (!q) return true;
        return `${item.name} ${item.description} ${item.meta} ${item.status}`.toLowerCase().includes(q);
      });
  }, [c, deferredQuery, filter, mode, view]);
  const itemRows = useMemo(() => chunkRows(items, 2), [items]);

  const preferredItems = useMemo(() => {
    const candidates = items.filter((item) => item.enabled && item.statusTone !== "warn");
    return candidates.length > 0 ? candidates : items;
  }, [items]);
  const heroItems = useMemo(() => preferredItems.slice(0, 5), [preferredItems]);
  const hero = heroItems[heroIndex] ?? heroItems[0];
  const HeroIcon = hero?.icon ?? Plug;
  const heroText = hero ? `${hero.description} · ${hero.status}` : c.heroFallback;

  useEffect(() => {
    setHeroIndex((index) => Math.min(index, Math.max(0, heroItems.length - 1)));
  }, [heroItems.length]);

  useEffect(() => {
    return () => {
      if (heroScrollFrameRef.current !== null) window.cancelAnimationFrame(heroScrollFrameRef.current);
    };
  }, []);

  const scrollHeroTo = useCallback(
    (index: number) => {
      if (heroItems.length === 0) return;
      const next = Math.max(0, Math.min(heroItems.length - 1, index));
      setHeroIndex(next);
      const track = heroTrackRef.current;
      const slide = track?.children.item(next) as HTMLElement | null;
      slide?.scrollIntoView({ behavior: "smooth", block: "nearest", inline: "center" });
    },
    [heroItems.length],
  );

  const handleHeroScroll = useCallback(() => {
    if (heroScrollFrameRef.current !== null) window.cancelAnimationFrame(heroScrollFrameRef.current);
    heroScrollFrameRef.current = window.requestAnimationFrame(() => {
      heroScrollFrameRef.current = null;
      const track = heroTrackRef.current;
      if (!track) return;
      const center = track.scrollLeft + track.clientWidth / 2;
      let closestIndex = 0;
      let closestDistance = Number.POSITIVE_INFINITY;
      Array.from(track.children).forEach((child, index) => {
        const element = child as HTMLElement;
        const childCenter = element.offsetLeft + element.offsetWidth / 2;
        const distance = Math.abs(childCenter - center);
        if (distance < closestDistance) {
          closestDistance = distance;
          closestIndex = index;
        }
      });
      setHeroIndex(closestIndex);
    });
  }, []);

  const handleHeroWheel = useCallback(
    (event: WheelEvent<HTMLDivElement>) => {
      const track = heroTrackRef.current;
      if (!track || heroItems.length < 2 || Math.abs(event.deltaY) <= Math.abs(event.deltaX)) return;
      event.preventDefault();
      track.scrollBy({ left: event.deltaY, behavior: "smooth" });
    },
    [heroItems.length],
  );

  const handleItemAction = useCallback(
    async (item: CatalogItem) => {
      setBusyItem(item.id);
      setLoadError(null);
      try {
        if (item.source === "server") {
          if (item.statusTone === "warn") await app.ReconnectMCPServer(item.name);
          else if (!item.enabled) await app.SetMCPServerEnabled(item.name, true);
          else if (item.removable) await app.RemoveMCPServer(item.name);
        } else {
          if (!item.enabled) await app.SetSkillEnabled(item.name, true);
          else if (item.removable) await app.RemoveSkill(item.name);
        }
        await loadCapabilities(true);
      } catch (error) {
        setLoadError(String((error as Error)?.message ?? error));
        await loadCapabilities(true);
      } finally {
        setBusyItem(null);
      }
    },
    [loadCapabilities],
  );

  const handleRefresh = useCallback(() => {
    void loadCapabilities();
  }, [loadCapabilities]);

  const openManager = useCallback(() => {
    setManagerMode(mode);
  }, [mode]);

  const openCreate = useCallback(() => {
    setManagerMode(mode);
    if (mode === "plugins") setAddServerRequest((value) => value + 1);
    else setAddSkillFocusRequest((value) => value + 1);
  }, [mode]);

  return (
    <main className="plugin-hub" aria-label={c.plugins} ref={hubRef}>
      <div className="plugin-hub__chrome">
        <div className="plugin-hub__tabs" role="tablist" aria-label={c.plugins}>
          <button
            type="button"
            role="tab"
            aria-selected={mode === "plugins"}
            className={`plugin-hub__tab${mode === "plugins" ? " plugin-hub__tab--active" : ""}`}
            onClick={() => setMode("plugins")}
          >
            {c.plugins}
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={mode === "skills"}
            className={`plugin-hub__tab${mode === "skills" ? " plugin-hub__tab--active" : ""}`}
            onClick={() => setMode("skills")}
          >
            {c.skills}
          </button>
        </div>
        <div className="plugin-hub__actions">
          <button type="button" className="plugin-hub__pill" onClick={openManager}>
            <Settings size={16} />
            <span>{c.manage}</span>
          </button>
          <button type="button" className="plugin-hub__pill" onClick={openCreate}>
            <span>{mode === "plugins" ? c.addPlugin : c.addSkill}</span>
            <ChevronDown size={15} />
          </button>
          <button type="button" className="plugin-hub__icon-btn" aria-label={c.refresh} onClick={handleRefresh} disabled={loading}>
            <RefreshCw size={18} className={loading ? "plugin-hub__spin" : undefined} />
          </button>
          <button type="button" className="plugin-hub__icon-btn" aria-label="More">
            <MoreHorizontal size={18} />
          </button>
        </div>
      </div>

      <section className="plugin-hub__body">
        <h1>{c.headline}</h1>
        <div className="plugin-hub__filters">
          <label className="plugin-hub__search">
            <Search size={18} />
            <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder={c.search} />
          </label>
          <button
            type="button"
            className={`plugin-hub__filter${filter === "builtIn" ? " plugin-hub__filter--active" : ""}`}
            onClick={() => setFilter(filter === "builtIn" ? "all" : "builtIn")}
          >
            <span>{c.builtIn}</span>
            <ChevronDown size={15} />
          </button>
          <button
            type="button"
            className={`plugin-hub__filter${filter === "enabled" ? " plugin-hub__filter--active" : ""}`}
            onClick={() => setFilter(filter === "enabled" ? "all" : "enabled")}
          >
            <span>{filter === "enabled" ? c.enabled : c.all}</span>
            <ChevronDown size={15} />
          </button>
        </div>

        {loadError ? (
          <div className="plugin-hub__notice" role="alert">
            <AlertTriangle size={17} />
            <span>{c.loadFailed}: {loadError}</span>
          </div>
        ) : null}

        <div className="plugin-hub__hero" aria-label={c.featured}>
          <div className="plugin-hub__hero-sheen" />
          <div
            ref={heroTrackRef}
            className="plugin-hub__hero-track"
            onScroll={handleHeroScroll}
            onWheel={handleHeroWheel}
            tabIndex={heroItems.length > 1 ? 0 : undefined}
          >
            {heroItems.length > 0 ? (
              heroItems.map((item, index) => {
                const Icon = item.icon;
                return (
                  <div className="plugin-hub__hero-slide" key={item.id} aria-hidden={index !== heroIndex}>
                    <div className="plugin-hub__hero-card">
                      <span className="plugin-hub__app-icon" style={{ "--plugin-color": item.color } as CSSProperties}>
                        <Icon size={20} />
                      </span>
                      <strong>{item.name}</strong>
                      <span>{`${item.description} · ${item.status}`}</span>
                      <button
                        type="button"
                        className="plugin-hub__hero-arrow"
                        onClick={() => onTry(item.prompt)}
                        tabIndex={index === heroIndex ? 0 : -1}
                        aria-label={c.tryInChat}
                      >
                        <ArrowRight size={17} />
                      </button>
                    </div>
                  </div>
                );
              })
            ) : (
              <div className="plugin-hub__hero-slide">
                <div className="plugin-hub__hero-card">
                  <span className="plugin-hub__app-icon" style={{ "--plugin-color": "#0088ff" } as CSSProperties}>
                    <HeroIcon size={20} />
                  </span>
                  <strong>Reasonix</strong>
                  <span>{heroText}</span>
                </div>
              </div>
            )}
          </div>
          <button type="button" className="plugin-hub__try" onClick={() => hero && onTry(hero.prompt)} disabled={!hero}>
            <Bot size={18} />
            <span>{c.tryInChat}</span>
          </button>
          {heroItems.length > 1 ? (
            <div className="plugin-hub__dots" aria-label={c.featured}>
              {heroItems.map((item, index) => (
                <button
                  type="button"
                  key={item.id}
                  className={`plugin-hub__dot${index === heroIndex ? " plugin-hub__dot--active" : ""}`}
                  aria-label={`${c.showFeatured}: ${item.name}`}
                  aria-current={index === heroIndex}
                  onClick={() => scrollHeroTo(index)}
                />
              ))}
            </div>
          ) : null}
        </div>

        {managerMode ? (
          <section className="plugin-hub__manager" aria-label={managerMode === "plugins" ? c.managerTitlePlugins : c.managerTitleSkills}>
            <div className="plugin-hub__manager-head">
              <div>
                <div className="plugin-hub__manager-title">
                  {managerMode === "plugins" ? c.managerTitlePlugins : c.managerTitleSkills}
                </div>
                <div className="plugin-hub__manager-desc">
                  {managerMode === "plugins" ? c.managerDescPlugins : c.managerDescSkills}
                </div>
              </div>
              <button type="button" className="plugin-hub__manager-close" onClick={() => setManagerMode(null)} aria-label={c.closeManager}>
                <X size={17} />
              </button>
            </div>
            <div className="plugin-hub__manager-body">
              {managerMode === "plugins" ? (
                <MCPServersSettingsPage addRequest={addServerRequest} />
              ) : (
                <SkillsSettingsPage focusAddRequest={addSkillFocusRequest} />
              )}
            </div>
          </section>
        ) : null}

        <div className="plugin-hub__section-title">{c.live}</div>
        {loading ? (
          <div className="plugin-hub__empty">
            <LoaderCircle size={18} className="plugin-hub__spin" />
            <span>{c.loading}</span>
          </div>
        ) : items.length === 0 ? (
          <div className="plugin-hub__empty">
            <strong>{query ? c.noResults : c.emptyTitle}</strong>
            <span>{query ? "" : c.emptyBody}</span>
          </div>
        ) : (
          <div className="plugin-hub__grid plugin-hub__grid--virtual">
            <VirtualList
              items={itemRows}
              scrollRef={hubRef}
              estimateSize={92}
              getKey={(row) => row.map((item) => item.id).join("|")}
              render={(row) => (
                <div className="plugin-hub__grid-row">
                  {row.map((item) => (
                    <CatalogCard key={item.id} item={item} busy={busyItem === item.id} onAction={handleItemAction} />
                  ))}
                </div>
              )}
            />
          </div>
        )}
      </section>
    </main>
  );
}

function chunkRows<T>(items: T[], size: number): T[][] {
  const rows: T[][] = [];
  for (let i = 0; i < items.length; i += size) rows.push(items.slice(i, i + size));
  return rows;
}

const CatalogCard = memo(function CatalogCard({
  item,
  busy,
  onAction,
}: {
  item: CatalogItem;
  busy: boolean;
  onAction: (item: CatalogItem) => void;
}) {
  const Icon = item.icon;
  const canRemove = item.enabled && item.statusTone !== "warn" && item.removable;
  const canAct = item.statusTone === "warn" || !item.enabled || canRemove;
  const actionClass = [
    "plugin-hub__item-action",
    busy ? "plugin-hub__item-action--busy" : "",
    canRemove ? "plugin-hub__item-action--remove" : "",
    !canAct ? "plugin-hub__item-action--static" : "",
  ].filter(Boolean).join(" ");
  return (
    <article className="plugin-hub__item">
      <span className="plugin-hub__app-icon" style={{ "--plugin-color": item.color } as CSSProperties}>
        <Icon size={21} />
      </span>
      <span className="plugin-hub__item-copy">
        <strong>{item.name}</strong>
        <span>{item.description}</span>
        <small className="plugin-hub__item-meta">
          <span className={`plugin-hub__status plugin-hub__status--${item.statusTone}`}>{item.status}</span>
          <span>{item.meta}</span>
        </small>
      </span>
      <button
        type="button"
        className={actionClass}
        onClick={() => void onAction(item)}
        disabled={busy || !canAct}
        aria-label={`${item.actionLabel} ${item.name}`}
      >
        {busy ? (
          <LoaderCircle size={18} />
        ) : item.statusTone === "warn" ? (
          <RefreshCw size={18} />
        ) : item.enabled ? (
          <>
            <Check className="plugin-hub__item-action-icon plugin-hub__item-action-icon--check" size={18} />
            {canRemove ? <X className="plugin-hub__item-action-icon plugin-hub__item-action-icon--remove" size={18} /> : null}
          </>
        ) : (
          <Plus size={18} />
        )}
      </button>
    </article>
  );
});

function buildCatalogItems(view: CapabilitiesView, c: ReturnType<typeof copy>): CatalogItem[] {
  const servers = view.servers.map((server) => serverToItem(server, c));
  const skills = view.skills.map((skill) => skillToItem(skill, c));
  return [...servers, ...skills];
}

function serverToItem(server: ServerView, c: ReturnType<typeof copy>): CatalogItem {
  const statusTone = serverStatusTone(server);
  const enabled = server.status !== "disabled";
  const removable = !server.builtIn && Boolean(server.configured);
  const counts = [
    server.tools > 0 ? `${server.tools} ${c.tools}` : "",
    server.prompts > 0 ? `${server.prompts} ${c.prompts}` : "",
    server.resources > 0 ? `${server.resources} ${c.resources}` : "",
  ].filter(Boolean);
  const description = describeServer(server, c);
  const status = serverStatusLabel(server, c);
  return {
    id: `server:${server.name}`,
    name: server.name,
    description,
    meta: [c.mcp, server.transport || "stdio", counts.join(" · ")].filter(Boolean).join(" · "),
    status,
    statusTone,
    enabled,
    builtIn: Boolean(server.builtIn),
    color: colorForName(server.name, statusTone),
    icon: iconForServer(server.name),
    prompt: `使用 ${server.name} MCP 插件处理当前任务。`,
    actionLabel: statusTone === "warn" ? c.retry : enabled ? (removable ? c.removePlugin : c.cannotRemove) : c.enable,
    kind: "plugins",
    source: "server",
    removable,
  };
}

function skillToItem(skill: SkillView, c: ReturnType<typeof copy>): CatalogItem {
  const scope = scopeLabel(skill.scope, c);
  const enabled = skill.enabled !== false;
  const removable = Boolean(skill.removable ?? (skill.scope !== "builtin" && skill.path));
  return {
    id: `skill:${skill.name}`,
    name: skill.name,
    description: summarizeText(skill.description, c.noDescription),
    meta: [c.skill, scope, skill.runAs].filter(Boolean).join(" · "),
    status: enabled ? c.skillEnabled : c.skillDisabled,
    statusTone: enabled ? "ok" : "off",
    enabled,
    builtIn: skill.scope === "builtin" || skill.scope === "global",
    color: colorForName(skill.name, enabled ? "ok" : "off"),
    icon: iconForSkill(skill.name),
    prompt: `使用 ${skill.name} 技能处理当前任务。`,
    actionLabel: enabled ? (removable ? c.removeSkill : c.cannotRemove) : c.enable,
    kind: "skills",
    source: "skill",
    removable,
  };
}

function describeServer(server: ServerView, c: ReturnType<typeof copy>): string {
  if (server.error) return summarizeText(server.error, c.noDescription);
  const toolDescription = server.toolList?.find((tool) => tool.description?.trim())?.description;
  if (toolDescription) return summarizeText(toolDescription, c.noDescription);
  if (server.url) return summarizeText(server.url, c.noDescription);
  if (server.command) return summarizeText([server.command, ...(server.args ?? [])].join(" "), c.noDescription);
  return c.noDescription;
}

function serverStatusLabel(server: ServerView, c: ReturnType<typeof copy>): string {
  if (server.status === "connected") return c.connected;
  if (server.status === "failed") return c.failed;
  if (server.status === "initializing") return c.initializing;
  if (server.status === "deferred") return c.deferred;
  if (server.status === "disabled") return c.disabled;
  return server.status || c.disabled;
}

function serverStatusTone(server: ServerView): StatusTone {
  if (server.status === "connected") return "ok";
  if (server.status === "failed" || server.authStatus === "required") return "warn";
  if (server.status === "disabled") return "off";
  return "info";
}

function scopeLabel(scope: string, c: ReturnType<typeof copy>): string {
  if (scope === "builtin" || scope === "global") return c.builtin;
  if (scope === "project") return c.project;
  if (scope === "custom") return c.custom;
  return scope || c.local;
}

function summarizeText(value: string | undefined, fallback: string): string {
  const normalized = (value ?? "").replace(/\s+/g, " ").trim();
  if (!normalized) return fallback;
  if (normalized.length <= 112) return normalized;
  return `${normalized.slice(0, 109).trim()}...`;
}

function colorForName(name: string, tone: StatusTone): string {
  const key = name.toLowerCase();
  if (key.includes("github")) return "#ffffff";
  if (key.includes("chrome") || key.includes("browser")) return "#4f8cff";
  if (key.includes("figma") || key.includes("design")) return "#a855f7";
  if (key.includes("mail") || key.includes("gmail")) return "#ea4335";
  if (key.includes("drive") || key.includes("file")) return "#34a853";
  if (key.includes("sheet") || key.includes("table")) return "#32a852";
  if (key.includes("presentation") || key.includes("slide")) return "#d2922b";
  if (key.includes("data") || key.includes("analytics")) return "#7ba7ff";
  if (tone === "warn") return "#f59e0b";
  if (tone === "off") return "#8a93a4";
  return "#0088ff";
}

function iconForServer(name: string): LucideIcon {
  const key = name.toLowerCase();
  if (key.includes("github")) return Puzzle;
  if (key.includes("chrome")) return Globe2;
  if (key.includes("figma")) return Palette;
  if (key.includes("gmail") || key.includes("mail")) return Mail;
  if (key.includes("drive") || key.includes("file")) return FolderOpen;
  if (key.includes("browser") || key.includes("web")) return Globe2;
  if (key.includes("code") || key.includes("graph")) return Puzzle;
  if (key.includes("data") || key.includes("db")) return Database;
  return Plug;
}

function iconForSkill(name: string): LucideIcon {
  const key = name.toLowerCase();
  if (key.includes("review") || key.includes("doc")) return FileText;
  if (key.includes("github") || key.includes("publish")) return Puzzle;
  if (key.includes("data") || key.includes("analytics")) return Database;
  if (key.includes("sheet") || key.includes("table")) return Table2;
  if (key.includes("presentation") || key.includes("slide")) return Presentation;
  if (key.includes("design") || key.includes("figma")) return Palette;
  if (key.includes("agent") || key.includes("explore")) return Bot;
  if (key.includes("openai") || key.includes("api")) return Sparkles;
  return Wrench;
}
