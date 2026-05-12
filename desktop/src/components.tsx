import {
  AlertTriangle,
  ArrowDown,
  ArrowRight,
  ArrowUp,
  AtSign,
  Check,
  CheckCircle2,
  ChevronRight,
  ClipboardCheck,
  Command,
  Copy,
  Download,
  ExternalLink,
  GitBranch,
  KeyRound,
  Loader2,
  MessageSquarePlus,
  RefreshCcw,
  Settings,
  ShieldCheck,
  Slash,
  Sparkles,
  Square,
  Wallet as WalletIcon,
  X,
  XCircle,
  Zap,
} from "lucide-react";
import { t, useLang, setLang, type Lang } from "./i18n";
import {
  type KeyboardEvent,
  type ReactNode,
  type RefObject,
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
} from "react";
import { Markdown } from "./Markdown";
import { getToolDef, previewFor, renderToolBody, summaryFor } from "./toolRenderers";

function relativeTime(iso: string): string {
  const ms = Date.now() - new Date(iso).getTime();
  const min = ms / 60_000;
  if (min < 1) return "just now";
  if (min < 60) return `${Math.floor(min)}m`;
  const hr = min / 60;
  if (hr < 24) return `${Math.floor(hr)}h`;
  const d = hr / 24;
  if (d < 7) return `${Math.floor(d)}d`;
  if (d < 30) return `${Math.floor(d / 7)}w`;
  return `${Math.floor(d / 30)}mo`;
}

export function TabBar({
  tabs,
  activeId,
  onActivate,
  onClose,
  onNew,
  singleTab,
}: {
  tabs: { id: string; workspaceDir?: string }[];
  activeId: string;
  onActivate: (id: string) => void;
  onClose: (id: string) => void;
  onNew: () => void;
  singleTab?: boolean;
}) {
  return (
    <div className={`tab-bar ${singleTab ? "single" : ""}`}>
      {!singleTab && (
        <div className="tab-bar-tabs">
          {tabs.map((t) => {
            const ws = t.workspaceDir ?? "";
            const label =
              ws.replace(/[\\/]$/, "").split(/[\\/]/).pop() || "workspace";
            return (
              <div
                key={t.id}
                className={`tab-bar-tab ${t.id === activeId ? "active" : ""}`}
                onClick={() => onActivate(t.id)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") onActivate(t.id);
                }}
                role="button"
                tabIndex={0}
                title={ws}
              >
                <GitBranch size={11} strokeWidth={2.2} />
                <span className="tab-bar-tab-label">{label}</span>
                {tabs.length > 1 && (
                  <button
                    type="button"
                    className="tab-bar-close"
                    onClick={(e) => {
                      e.stopPropagation();
                      onClose(t.id);
                    }}
                    aria-label="close tab"
                  >
                    <X size={10} />
                  </button>
                )}
              </div>
            );
          })}
        </div>
      )}
      <button
        type="button"
        className="tab-bar-new"
        onClick={onNew}
        aria-label="new tab"
        title="New tab"
      >
        <span className="tab-bar-new-plus">+</span>
        {singleTab && <span className="tab-bar-new-text">new tab</span>}
      </button>
    </div>
  );
}

export function Sidebar({
  sessions,
  version,
  balance,
  onNewChat,
  onOpenCommands,
  onOpenSettings,
  onDeleteSession,
  onLoadSession,
}: {
  sessions: { name: string; messageCount: number; mtime: string }[];
  version?: string;
  balance: { currency: string; total: number; isAvailable: boolean } | null;
  onNewChat: () => void;
  onOpenCommands: () => void;
  onOpenSettings: () => void;
  onDeleteSession: (name: string) => void;
  onLoadSession: (name: string) => void;
}) {
  useLang();
  return (
    <aside className="sidebar">
      <div className="sidebar-brand">
        <div className="brand-mark">R</div>
        <div className="sidebar-brand-text">
          <span className="sidebar-brand-name">Reasonix</span>
          {version && <span className="sidebar-brand-meta">v{version}</span>}
        </div>
      </div>
      <div className="sidebar-body">
        <button type="button" className="sidebar-new" onClick={onNewChat}>
          <MessageSquarePlus size={14} strokeWidth={2.2} />
          <span>{t("sidebar.newChat")}</span>
          <span className="sidebar-new-kbd">
            <span className="kbd">⌘</span>
            <span className="kbd">N</span>
          </span>
        </button>
        <button type="button" className="sidebar-cmdk" onClick={onOpenCommands}>
          <Command size={13} />
          <span>{t("sidebar.searchCommands")}</span>
          <span className="kbd">⌘K</span>
        </button>
        <div className="sidebar-section">
          <div className="sidebar-section-label">
            {t("sidebar.recent")} {sessions.length > 0 && <span className="sidebar-count">{sessions.length}</span>}
          </div>
          {sessions.length === 0 ? (
            <div className="sidebar-empty">{t("sidebar.noSessions")}</div>
          ) : (
            <div className="sidebar-list">
              {sessions.map((s) => (
                <button
                  key={s.name}
                  type="button"
                  className="sidebar-item"
                  onClick={() => onLoadSession(s.name)}
                >
                  <div className="sidebar-item-main">
                    <div className="sidebar-item-name">{s.name}</div>
                    <div className="sidebar-item-meta">
                      {s.messageCount} msg · {relativeTime(s.mtime)}
                    </div>
                  </div>
                  <span
                    role="button"
                    tabIndex={0}
                    className="sidebar-item-del"
                    aria-label="delete session"
                    onClick={(e) => {
                      e.stopPropagation();
                      onDeleteSession(s.name);
                    }}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") {
                        e.stopPropagation();
                        onDeleteSession(s.name);
                      }
                    }}
                  >
                    <X size={11} strokeWidth={2.4} />
                  </span>
                </button>
              ))}
            </div>
          )}
        </div>
        {balance && (
          <button
            type="button"
            className={`sidebar-wallet ${balance.isAvailable ? "" : "warn"}`}
            onClick={onOpenSettings}
            title={
              balance.isAvailable
                ? `DeepSeek wallet · ${formatWallet(balance.total, balance.currency)} remaining`
                : "Account flagged not-available — top up at platform.deepseek.com"
            }
          >
            <WalletIcon size={14} strokeWidth={2} />
            <span className="sidebar-wallet-label">{t("sidebar.wallet")}</span>
            <span className="sidebar-wallet-amount">
              {formatWallet(balance.total, balance.currency)}
            </span>
          </button>
        )}
        <div className="sidebar-foot">
          <button
            type="button"
            className="icon-btn"
            aria-label="settings"
            title={t("palette.settings")}
            onClick={onOpenSettings}
          >
            <Settings size={14} />
          </button>
          <span className="sidebar-foot-hint">{t("sidebar.footHint")}</span>
        </div>
      </div>
    </aside>
  );
}

const USD_TO_CNY = 7.2;

function formatCost(usd: number, currency: "CNY" | "USD" = "CNY"): string {
  if (currency === "USD") {
    if (usd < 0.001) return `$${usd.toFixed(6).replace(/0+$/, "").replace(/\.$/, "")}`;
    if (usd < 1) return `$${usd.toFixed(4)}`;
    return `$${usd.toFixed(3)}`;
  }
  const cny = usd * USD_TO_CNY;
  if (cny < 0.01) return `¥${cny.toFixed(5).replace(/0+$/, "").replace(/\.$/, "")}`;
  if (cny < 1) return `¥${cny.toFixed(3)}`;
  return `¥${cny.toFixed(2)}`;
}

function formatTokens(n: number): string {
  if (n < 1000) return n.toString();
  if (n < 1_000_000) return `${(n / 1000).toFixed(n < 10_000 ? 1 : 0)}k`;
  return `${(n / 1_000_000).toFixed(2)}M`;
}

function currencySymbol(code: string): string {
  switch (code.toUpperCase()) {
    case "CNY":
      return "¥";
    case "USD":
      return "$";
    case "EUR":
      return "€";
    case "GBP":
      return "£";
    case "JPY":
      return "¥";
    default:
      return "";
  }
}

function formatWallet(total: number, code: string): string {
  const sym = currencySymbol(code);
  const fixed = total >= 1000 ? total.toFixed(0) : total < 1 ? total.toFixed(3) : total.toFixed(2);
  return sym ? `${sym}${fixed}` : `${fixed} ${code}`;
}

const PRESET_OPTIONS: { id: "auto" | "flash" | "pro"; label: string; hint: string }[] = [
  { id: "auto", label: "auto", hint: "flash → pro on hard turns" },
  { id: "flash", label: "flash", hint: "v4-flash always · cheapest" },
  { id: "pro", label: "pro", hint: "v4-pro always · ~12× cost" },
];

function workspaceBasename(p?: string): string | null {
  if (!p) return null;
  const norm = p.replace(/\\/g, "/").replace(/\/$/, "");
  const last = norm.split("/").pop();
  return last || norm;
}

export function Header({
  model,
  preset,
  editMode,
  workspaceDir,
  recentWorkspaces,
  streaming,
  turnCount,
  usage,
  onOpenCommands,
  onPickPreset,
  onPickEditMode,
  onPickWorkspace,
  onSwitchWorkspace,
  currency,
}: {
  model?: string;
  preset: "auto" | "flash" | "pro";
  editMode: "review" | "auto" | "yolo";
  workspaceDir?: string;
  recentWorkspaces: string[];
  streaming: boolean;
  turnCount: number;
  usage: {
    totalCostUsd: number;
    totalPromptTokens: number;
    totalCompletionTokens: number;
    cacheHitTokens: number;
    cacheMissTokens: number;
    lastCallCacheHit: number | null;
    lastCallCacheMiss: number | null;
  };
  onOpenCommands: () => void;
  onPickPreset: (p: "auto" | "flash" | "pro") => void;
  onPickEditMode: (m: "review" | "auto" | "yolo") => void;
  onPickWorkspace: () => void;
  onSwitchWorkspace: (dir: string) => void;
  currency: "CNY" | "USD";
}) {
  const totalTokens = usage.totalPromptTokens + usage.totalCompletionTokens;
  const cumHit = usage.cacheHitTokens;
  const cumMiss = usage.cacheMissTokens;
  const cumCacheTotal = cumHit + cumMiss;
  const cumCacheRatio = cumCacheTotal > 0 ? cumHit / cumCacheTotal : null;
  const lastHit = usage.lastCallCacheHit ?? 0;
  const lastMiss = usage.lastCallCacheMiss ?? 0;
  const lastTotal = lastHit + lastMiss;
  const lastRatio = lastTotal > 0 ? lastHit / lastTotal : null;
  const cacheRatio = lastRatio ?? cumCacheRatio;
  const [presetOpen, setPresetOpen] = useState(false);
  const presetRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    if (!presetOpen) return;
    const onDown = (e: globalThis.MouseEvent) => {
      if (!presetRef.current?.contains(e.target as Node)) setPresetOpen(false);
    };
    window.addEventListener("mousedown", onDown);
    return () => window.removeEventListener("mousedown", onDown);
  }, [presetOpen]);
  const wsName = workspaceBasename(workspaceDir);
  const [wsOpen, setWsOpen] = useState(false);
  const wsRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    if (!wsOpen) return;
    const onDown = (e: globalThis.MouseEvent) => {
      if (!wsRef.current?.contains(e.target as Node)) setWsOpen(false);
    };
    window.addEventListener("mousedown", onDown);
    return () => window.removeEventListener("mousedown", onDown);
  }, [wsOpen]);
  const [editModeOpen, setEditModeOpen] = useState(false);
  const editModeRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    if (!editModeOpen) return;
    const onDown = (e: globalThis.MouseEvent) => {
      if (!editModeRef.current?.contains(e.target as Node)) setEditModeOpen(false);
    };
    window.addEventListener("mousedown", onDown);
    return () => window.removeEventListener("mousedown", onDown);
  }, [editModeOpen]);
  useLang();
  return (
    <div className="header">
      <div className="header-title">
        <div className="header-title-main">
          {streaming ? "Thinking…" : turnCount === 0 ? "New conversation" : "Conversation"}
        </div>
        <div className="ws-pill-wrap" ref={wsRef}>
          <button
            type="button"
            className="ws-pill"
            onClick={() => setWsOpen((v) => !v)}
            title={workspaceDir ?? "Pick workspace"}
          >
            <GitBranch size={11} strokeWidth={2.2} />
            <span className="header-ws-name">{wsName ?? "no workspace"}</span>
            <ChevronRight
              size={10}
              strokeWidth={2.4}
              style={{ transform: wsOpen ? "rotate(90deg)" : "rotate(0deg)", transition: "transform 120ms ease" }}
            />
          </button>
          {wsOpen && (
            <div className="ws-menu">
              <div className="ws-menu-section">
                <div className="ws-menu-label">current</div>
                <div className="ws-menu-current mono" title={workspaceDir}>
                  {workspaceDir ?? "(unset)"}
                </div>
              </div>
              {recentWorkspaces.length > 0 && (
                <div className="ws-menu-section">
                  <div className="ws-menu-label">recent</div>
                  {recentWorkspaces.map((dir) => (
                    <button
                      type="button"
                      key={dir}
                      className="ws-menu-item"
                      title={dir}
                      onClick={() => {
                        setWsOpen(false);
                        onSwitchWorkspace(dir);
                      }}
                    >
                      <span className="ws-menu-item-name">
                        {workspaceBasename(dir) ?? dir}
                      </span>
                      <span className="ws-menu-item-path mono">{dir}</span>
                    </button>
                  ))}
                </div>
              )}
              <div className="ws-menu-section">
                <button
                  type="button"
                  className="ws-menu-pick"
                  onClick={() => {
                    setWsOpen(false);
                    onPickWorkspace();
                  }}
                >
                  Pick another folder…
                </button>
              </div>
            </div>
          )}
        </div>
        {turnCount > 0 && (
          <div className="header-title-meta">
            {turnCount} turn{turnCount === 1 ? "" : "s"}
          </div>
        )}
      </div>
      <div className="header-meta">
        {totalTokens > 0 && (
          <span className="badge stat" title={`${usage.totalPromptTokens.toLocaleString()} prompt · ${usage.totalCompletionTokens.toLocaleString()} completion`}>
            <span className="stat-num">{formatTokens(totalTokens)}</span>
            <span className="stat-label">tok</span>
          </span>
        )}
        {cacheRatio !== null && (
          <span
            className="badge stat cache"
            title={
              lastRatio !== null
                ? `Last API call: ${lastHit.toLocaleString()} hit / ${lastTotal.toLocaleString()} prompt tokens (${(lastRatio * 100).toFixed(1)}%).\nSession cumulative: ${cumHit.toLocaleString()} / ${cumCacheTotal.toLocaleString()} (${cumCacheRatio !== null ? (cumCacheRatio * 100).toFixed(1) : "—"}%).\nFirst call of a fresh prefix is always 0% — cache builds over turns.`
                : "no API calls yet"
            }
          >
            <span className="stat-num">{(cacheRatio * 100).toFixed(0)}%</span>
            <span className="stat-label">cache</span>
          </span>
        )}
        {usage.totalCostUsd > 0 && (
          <span
            className="badge stat cost"
            title={`Based on reference rates (cache hit / miss / output ¥/M tokens). Converted at fixed 7.2 FX. May differ from DeepSeek dashboard if pricing changed.`}
          >
            <span className="stat-num">{formatCost(usage.totalCostUsd, currency)}</span>
          </span>
        )}
        <div className="preset-pill-wrap" ref={editModeRef}>
          <button
            type="button"
            className={`badge edit-pill edit-pill-${editMode}`}
            onClick={() => setEditModeOpen((v) => !v)}
            title={t("editMode.label")}
          >
            {editMode === "review" ? (
              <ShieldCheck size={11} strokeWidth={2.4} />
            ) : editMode === "yolo" ? (
              <Zap size={11} strokeWidth={2.4} />
            ) : (
              <Sparkles size={11} strokeWidth={2.4} />
            )}
            <span className="edit-pill-label">{t(`editMode.${editMode}` as const)}</span>
            <ChevronRight
              size={11}
              strokeWidth={2.4}
              style={{ transform: editModeOpen ? "rotate(90deg)" : "rotate(0deg)", transition: "transform 120ms ease" }}
            />
          </button>
          {editModeOpen && (
            <div className="preset-menu">
              {(["review", "auto", "yolo"] as const).map((opt) => (
                <button
                  type="button"
                  key={opt}
                  className={`preset-menu-item ${opt === editMode ? "active" : ""}`}
                  onClick={() => {
                    if (opt !== editMode) onPickEditMode(opt);
                    setEditModeOpen(false);
                  }}
                >
                  <span className="preset-menu-label">{t(`editMode.${opt}` as const)}</span>
                  <span className="preset-menu-hint">
                    {opt === "review"
                      ? t("editMode.reviewDesc")
                      : opt === "auto"
                        ? t("editMode.autoDesc")
                        : t("editMode.yoloDesc")}
                  </span>
                </button>
              ))}
            </div>
          )}
        </div>
        <div className="preset-pill-wrap" ref={presetRef}>
          <button
            type="button"
            className={`badge accent preset-pill ${streaming ? "streaming" : ""}`}
            onClick={() => setPresetOpen((v) => !v)}
            title={model ? `model: ${model}` : "preset"}
          >
            <span className="badge-dot" />
            {preset}
            <ChevronRight
              size={11}
              strokeWidth={2.4}
              style={{ transform: presetOpen ? "rotate(90deg)" : "rotate(0deg)", transition: "transform 120ms ease" }}
            />
          </button>
          {presetOpen && (
            <div className="preset-menu">
              {PRESET_OPTIONS.map((opt) => (
                <button
                  type="button"
                  key={opt.id}
                  className={`preset-menu-item ${opt.id === preset ? "active" : ""}`}
                  onClick={() => {
                    if (opt.id !== preset) onPickPreset(opt.id);
                    setPresetOpen(false);
                  }}
                >
                  <span className="preset-menu-label">{opt.label}</span>
                  <span className="preset-menu-hint">{opt.hint}</span>
                </button>
              ))}
            </div>
          )}
        </div>
        <button
          type="button"
          className="icon-btn"
          onClick={onOpenCommands}
          aria-label="commands"
        >
          <Command size={14} />
        </button>
      </div>
    </div>
  );
}

export function SettingsPanel({
  settings,
  onSave,
  onSaveApiKey,
  onClose,
  onPickWorkspace,
}: {
  settings: {
    reasoningEffort: "high" | "max";
    editMode: "review" | "auto" | "yolo";
    budgetUsd: number | null;
    baseUrl?: string;
    apiKeyPrefix?: string;
    workspaceDir: string;
    model: string;
    editor?: string;
  };
  onSave: (patch: {
    reasoningEffort?: "high" | "max";
    editMode?: "review" | "auto" | "yolo";
    budgetUsd?: number | null;
    baseUrl?: string;
    workspaceDir?: string;
    editor?: string;
  }) => void;
  onSaveApiKey: (key: string) => void;
  onClose: () => void;
  onPickWorkspace: () => void;
}) {
  const [budget, setBudget] = useState<string>(
    settings.budgetUsd === null ? "" : String(settings.budgetUsd),
  );
  const [baseUrl, setBaseUrl] = useState<string>(settings.baseUrl ?? "");
  const [keyEditing, setKeyEditing] = useState(false);
  const [keyDraft, setKeyDraft] = useState("");
  const [keyVisible, setKeyVisible] = useState(false);
  const [editorDraft, setEditorDraft] = useState<string>(settings.editor ?? "");
  const editorPresets: { id: string; label: string; cmd: string }[] = [
    { id: "system", label: t("settings.editorSystem"), cmd: "" },
    { id: "vscode", label: "VS Code", cmd: "code" },
    { id: "cursor", label: "Cursor", cmd: "cursor" },
    { id: "windsurf", label: "Windsurf", cmd: "windsurf" },
    { id: "subl", label: "Sublime Text", cmd: "subl" },
    { id: "idea", label: "JetBrains", cmd: "idea" },
  ];
  const currentEditorPreset = editorPresets.find((p) => p.cmd === (settings.editor ?? ""));
  const [theme, setTheme] = useState<"dark" | "light">(() => {
    const v = localStorage.getItem("reasonix.theme");
    return v === "light" ? "light" : "dark";
  });
  const applyTheme = (next: "dark" | "light") => {
    setTheme(next);
    localStorage.setItem("reasonix.theme", next);
    document.documentElement.dataset.theme = next;
  };
  const [currency, setCurrencyState] = useState<"CNY" | "USD">(() => {
    const v = localStorage.getItem("reasonix.currency");
    return v === "USD" ? "USD" : "CNY";
  });
  const applyCurrency = (next: "CNY" | "USD") => {
    setCurrencyState(next);
    localStorage.setItem("reasonix.currency", next);
    window.dispatchEvent(new CustomEvent("reasonix:currency", { detail: next }));
  };
  const lang = useLang();
  const applyLang = (next: Lang) => {
    setLang(next);
  };
  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);
  return (
    <div className="settings-overlay" onMouseDown={onClose}>
      <div className="settings-panel" onMouseDown={(e) => e.stopPropagation()}>
        <div className="settings-head">
          <div className="settings-title">{t("settings.title")}</div>
          <button type="button" className="icon-btn" onClick={onClose} aria-label={t("settings.close")}>
            <X size={14} />
          </button>
        </div>
        <div className="settings-body">
          <SettingsSection
            label={t("settings.costCurrency")}
            hint={t("settings.costCurrencyHint")}
          >
            <div className="settings-radio-group">
              {(["CNY", "USD"] as const).map((opt) => (
                <button
                  key={opt}
                  type="button"
                  className={`settings-radio ${currency === opt ? "on" : ""}`}
                  onClick={() => applyCurrency(opt)}
                >
                  <span className="settings-radio-name">{opt === "CNY" ? "¥ CNY" : "$ USD"}</span>
                  <span className="settings-radio-desc">
                    {opt === "CNY" ? t("settings.cnyDesc") : t("settings.usdDesc")}
                  </span>
                </button>
              ))}
            </div>
          </SettingsSection>

          <SettingsSection label={t("settings.language")} hint={t("settings.languageHint")}>
            <div className="settings-radio-group">
              {(["en", "zh-CN"] as const).map((opt) => (
                <button
                  key={opt}
                  type="button"
                  className={`settings-radio ${lang === opt ? "on" : ""}`}
                  onClick={() => applyLang(opt)}
                >
                  <span className="settings-radio-name">
                    {opt === "en" ? t("settings.langEn") : t("settings.langZhCn")}
                  </span>
                  <span className="settings-radio-desc">
                    {opt === "en" ? t("settings.langEnDesc") : t("settings.langZhCnDesc")}
                  </span>
                </button>
              ))}
            </div>
          </SettingsSection>

          <SettingsSection label={t("settings.theme")} hint={t("settings.themeHint")}>
            <div className="settings-radio-group">
              {(["dark", "light"] as const).map((opt) => (
                <button
                  key={opt}
                  type="button"
                  className={`settings-radio ${theme === opt ? "on" : ""}`}
                  onClick={() => applyTheme(opt)}
                >
                  <span className="settings-radio-name">
                    {opt === "dark" ? t("settings.themeDark") : t("settings.themeLight")}
                  </span>
                  <span className="settings-radio-desc">
                    {opt === "dark" ? t("settings.themeDarkDesc") : t("settings.themeLightDesc")}
                  </span>
                </button>
              ))}
            </div>
          </SettingsSection>

          <SettingsSection
            label={t("settings.reasoningEffort")}
            hint={t("settings.reasoningEffortHint")}
          >
            <div className="settings-radio-group">
              {(["high", "max"] as const).map((opt) => (
                <button
                  key={opt}
                  type="button"
                  className={`settings-radio ${settings.reasoningEffort === opt ? "on" : ""}`}
                  onClick={() => onSave({ reasoningEffort: opt })}
                >
                  <span className="settings-radio-name">
                    {opt === "high" ? t("settings.effortHigh") : t("settings.effortMax")}
                  </span>
                  <span className="settings-radio-desc">
                    {opt === "high"
                      ? t("settings.effortHighDesc")
                      : t("settings.effortMaxDesc")}
                  </span>
                </button>
              ))}
            </div>
          </SettingsSection>

          <SettingsSection
            label={t("settings.editMode")}
            hint={t("settings.editModeHint")}
          >
            <div className="settings-radio-group">
              {(["review", "auto", "yolo"] as const).map((opt) => (
                <button
                  key={opt}
                  type="button"
                  className={`settings-radio ${settings.editMode === opt ? "on" : ""}`}
                  onClick={() => onSave({ editMode: opt })}
                >
                  <span className="settings-radio-name">
                    {opt === "review"
                      ? t("settings.editModeReview")
                      : opt === "auto"
                        ? t("settings.editModeAuto")
                        : t("settings.editModeYolo")}
                  </span>
                  <span className="settings-radio-desc">
                    {opt === "review"
                      ? t("settings.editModeReviewDesc")
                      : opt === "auto"
                        ? t("settings.editModeAutoDesc")
                        : t("settings.editModeYoloDesc")}
                  </span>
                </button>
              ))}
            </div>
          </SettingsSection>

          <SettingsSection label={t("settings.budget")} hint={t("settings.budgetHint")}>
            <div className="settings-input-row">
              <span className="settings-input-prefix">$</span>
              <input
                type="text"
                inputMode="decimal"
                className="settings-input"
                value={budget}
                placeholder={t("settings.budgetPlaceholder")}
                onChange={(e) => setBudget(e.target.value)}
                onBlur={() => {
                  const trimmed = budget.trim();
                  if (trimmed === "") {
                    onSave({ budgetUsd: null });
                    return;
                  }
                  const n = Number.parseFloat(trimmed);
                  if (Number.isFinite(n) && n > 0) onSave({ budgetUsd: n });
                }}
              />
            </div>
          </SettingsSection>

          <SettingsSection
            label={t("settings.baseUrl")}
            hint={t("settings.baseUrlHint")}
          >
            <input
              type="text"
              className="settings-input long"
              value={baseUrl}
              placeholder="https://api.deepseek.com"
              onChange={(e) => setBaseUrl(e.target.value)}
              onBlur={() => {
                if (baseUrl !== (settings.baseUrl ?? "")) {
                  onSave({ baseUrl });
                }
              }}
            />
          </SettingsSection>

          <SettingsSection
            label={t("settings.workspace")}
            hint={t("settings.workspaceHint")}
          >
            <div className="settings-workspace">
              <span className="settings-workspace-path mono" title={settings.workspaceDir}>
                {settings.workspaceDir}
              </span>
              <button
                type="button"
                className="settings-workspace-btn"
                onClick={onPickWorkspace}
              >
                {t("settings.workspaceChange")}
              </button>
            </div>
          </SettingsSection>

          <SettingsSection
            label={t("settings.editor")}
            hint={t("settings.editorHint")}
          >
            <div className="settings-radio-group editor-grid">
              {editorPresets.map((p) => (
                <button
                  key={p.id}
                  type="button"
                  className={`settings-radio ${currentEditorPreset?.id === p.id ? "on" : ""}`}
                  onClick={() => {
                    setEditorDraft(p.cmd);
                    onSave({ editor: p.cmd });
                  }}
                >
                  <span className="settings-radio-name">{p.label}</span>
                  <span className="settings-radio-desc mono">
                    {p.cmd || t("settings.editorDefault")}
                  </span>
                </button>
              ))}
            </div>
            <div className="settings-editor-custom">
              <span className="settings-meta-key">{t("settings.editorCustom")}</span>
              <input
                type="text"
                className="settings-input long"
                value={editorDraft}
                placeholder={t("settings.editorPlaceholder")}
                onChange={(e) => setEditorDraft(e.target.value)}
                onBlur={() => {
                  if (editorDraft !== (settings.editor ?? "")) {
                    onSave({ editor: editorDraft });
                  }
                }}
              />
            </div>
          </SettingsSection>

          <SettingsSection
            label={t("settings.apiKey")}
            hint={t("settings.apiKeyHint")}
          >
            {keyEditing ? (
              <div className="settings-key-edit">
                <div className="settings-key-input-row">
                  <input
                    type={keyVisible ? "text" : "password"}
                    autoFocus
                    className="settings-input long"
                    value={keyDraft}
                    placeholder="sk-..."
                    onChange={(e) => setKeyDraft(e.target.value)}
                    spellCheck={false}
                    autoCorrect="off"
                    autoCapitalize="off"
                  />
                  <button
                    type="button"
                    className="settings-key-toggle"
                    onClick={() => setKeyVisible((v) => !v)}
                  >
                    {keyVisible ? t("settings.apiKeyHide") : t("settings.apiKeyShow")}
                  </button>
                </div>
                <div className="settings-key-actions">
                  <button
                    type="button"
                    className="settings-key-cancel"
                    onClick={() => {
                      setKeyEditing(false);
                      setKeyDraft("");
                      setKeyVisible(false);
                    }}
                  >
                    {t("settings.apiKeyCancel")}
                  </button>
                  <button
                    type="button"
                    className="settings-key-save"
                    disabled={keyDraft.trim().length < 16}
                    onClick={() => {
                      onSaveApiKey(keyDraft.trim());
                      setKeyEditing(false);
                      setKeyDraft("");
                      setKeyVisible(false);
                    }}
                  >
                    {t("settings.apiKeySave")}
                  </button>
                </div>
              </div>
            ) : (
              <div className="settings-workspace">
                <span className="settings-workspace-path mono">
                  {settings.apiKeyPrefix ?? t("settings.apiKeyNotSet")}
                </span>
                <button
                  type="button"
                  className="settings-workspace-btn"
                  onClick={() => setKeyEditing(true)}
                >
                  {t("settings.workspaceChange")}
                </button>
              </div>
            )}
          </SettingsSection>

          <SettingsSection label={t("settings.environment")}>
            <div className="settings-meta">
              <div className="settings-meta-row">
                <span className="settings-meta-key">{t("settings.model")}</span>
                <span className="settings-meta-val mono">{settings.model}</span>
              </div>
            </div>
          </SettingsSection>
        </div>
      </div>
    </div>
  );
}

function SettingsSection({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <div className="settings-section">
      <div className="settings-section-head">
        <div className="settings-section-label">{label}</div>
        {hint && <div className="settings-section-hint">{hint}</div>}
      </div>
      <div className="settings-section-body">{children}</div>
    </div>
  );
}

export function UpdateBanner({
  version,
  currentVersion,
  body,
  status,
  onInstall,
  onDismiss,
}: {
  version: string;
  currentVersion: string;
  body?: string;
  status: "idle" | "installing" | "error";
  onInstall: () => void;
  onDismiss: () => void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <div className={`update-banner ${status}`}>
      <div className="update-banner-row">
        <div className="update-banner-icon">
          {status === "installing" ? (
            <Loader2 size={14} className="spin" strokeWidth={2.4} />
          ) : (
            <Sparkles size={14} strokeWidth={2.4} />
          )}
        </div>
        <div className="update-banner-text">
          <span className="update-banner-title">
            Reasonix <span className="update-banner-ver">{version}</span> is available
          </span>
          <span className="update-banner-sub">
            currently {currentVersion}
            {body && (
              <button
                type="button"
                className="update-banner-notes"
                onClick={() => setOpen((v) => !v)}
              >
                {open ? "hide notes" : "release notes"}
              </button>
            )}
          </span>
        </div>
        <div className="update-banner-actions">
          <button type="button" className="update-banner-later" onClick={onDismiss}>
            Later
          </button>
          <button
            type="button"
            className="update-banner-install"
            onClick={onInstall}
            disabled={status === "installing"}
          >
            <Download size={12} strokeWidth={2.4} />
            <span>
              {status === "installing"
                ? "Installing…"
                : status === "error"
                  ? "Retry"
                  : "Install & restart"}
            </span>
          </button>
        </div>
      </div>
      {open && body && <div className="update-banner-body">{body}</div>}
    </div>
  );
}

export function OnboardingScreen({
  onSubmit,
  workspaceDir,
  onPickWorkspace,
}: {
  onSubmit: (key: string) => void;
  workspaceDir?: string;
  onPickWorkspace: () => void;
}) {
  const [key, setKey] = useState("");
  const [visible, setVisible] = useState(false);
  const trimmed = key.trim();
  const canSubmit = trimmed.length >= 16;
  return (
    <div className="onboarding">
      <div className="onboarding-inner">
        <div className="onboarding-glyph">
          <KeyRound size={24} strokeWidth={2.2} />
        </div>
        <div className="onboarding-title-wrap">
          <div className="onboarding-eyebrow">
            <span className="empty-eyebrow-dot" /> first-time setup
          </div>
          <h1 className="onboarding-title">
            Paste your <span className="empty-grad">DeepSeek</span> API key
          </h1>
          <p className="onboarding-lede">
            Reasonix runs entirely on your key — it doesn't proxy through anyone.
            Generate one at{" "}
            <a
              href="https://platform.deepseek.com/api_keys"
              target="_blank"
              rel="noreferrer"
              className="onboarding-link"
            >
              platform.deepseek.com/api_keys
              <ExternalLink size={11} />
            </a>
            , paste below, done.
          </p>
        </div>
        <form
          className="onboarding-form"
          onSubmit={(e) => {
            e.preventDefault();
            if (canSubmit) onSubmit(trimmed);
          }}
        >
          <div className="onboarding-input-wrap">
            <input
              type={visible ? "text" : "password"}
              autoFocus
              className="onboarding-input"
              placeholder="sk-..."
              value={key}
              onChange={(e) => setKey(e.target.value)}
              spellCheck={false}
              autoCorrect="off"
              autoCapitalize="off"
            />
            <button
              type="button"
              className="onboarding-toggle"
              onClick={() => setVisible((v) => !v)}
              aria-label={visible ? "hide" : "show"}
            >
              {visible ? "hide" : "show"}
            </button>
          </div>
          <button type="submit" className="onboarding-submit" disabled={!canSubmit}>
            <Check size={14} strokeWidth={2.6} />
            <span>Save & continue</span>
          </button>
        </form>
        <div className="onboarding-workspace">
          <span className="onboarding-workspace-label">Workspace</span>
          <span
            className="onboarding-workspace-path mono"
            title={workspaceDir ?? "(detecting)"}
          >
            {workspaceDir ?? "(detecting)"}
          </span>
          <button
            type="button"
            className="onboarding-workspace-btn"
            onClick={onPickWorkspace}
          >
            Change…
          </button>
        </div>
        <div className="onboarding-foot">
          Saved to <code>~/.reasonix/settings.json</code> · readable only by you (0600 perms on unix).
        </div>
      </div>
    </div>
  );
}

export function EmptyState(_props: { onPick: (text: string) => void }) {
  return (
    <div className="empty">
      <div className="empty-inner">
        <div className="empty-eyebrow">
          <span className="empty-eyebrow-dot" />
          <span>ready</span>
          <span className="empty-eyebrow-sep">·</span>
          <span className="empty-eyebrow-mono">DeepSeek-native</span>
        </div>
        <WhaleScene />
        <h1 className="empty-headline">
          What can <span className="empty-grad">Reasonix</span>
          <br />
          do for you?
        </h1>
        <p className="empty-lede">
          Cache-first coding agent · runs cheaper by an order of magnitude.
        </p>
        <div className="empty-footer">
          <span className="empty-kbd">
            <span className="kbd">⌘</span>
            <span className="kbd">K</span>
            <span>commands</span>
          </span>
          <span className="empty-kbd">
            <Slash size={11} />
            <span>slash · type /</span>
          </span>
          <span className="empty-kbd">
            <AtSign size={11} />
            <span>mention · type @</span>
          </span>
        </div>
      </div>
    </div>
  );
}

function WhaleScene() {
  const uid = useId().replace(/:/g, "");
  const bodyId = `wb-${uid}`;
  const bellyId = `wbe-${uid}`;
  const dropId = `wd-${uid}`;
  return (
    <div className="whale-scene" aria-hidden="true">
      <div className="whale-logo">R</div>
      <svg viewBox="0 0 360 240" className="whale-svg" xmlns="http://www.w3.org/2000/svg">
        <defs>
          <linearGradient id={bodyId} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="var(--accent)" />
            <stop offset="100%" stopColor="var(--accent-2)" />
          </linearGradient>
          <linearGradient id={bellyId} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="var(--accent-2)" stopOpacity="0.55" />
            <stop offset="100%" stopColor="var(--accent-2)" stopOpacity="0.85" />
          </linearGradient>
          <radialGradient id={dropId} cx="0.5" cy="0.35" r="0.7">
            <stop offset="0%" stopColor="#ffffff" stopOpacity="0.95" />
            <stop offset="55%" stopColor="var(--accent)" stopOpacity="0.6" />
            <stop offset="100%" stopColor="var(--accent-2)" stopOpacity="0" />
          </radialGradient>
        </defs>
        <g className="whale-spout">
          <circle cx="135" cy="78" r="6" fill={`url(#${dropId})`} />
          <circle cx="128" cy="58" r="5" fill={`url(#${dropId})`} />
          <circle cx="138" cy="42" r="7" fill={`url(#${dropId})`} />
          <circle cx="146" cy="26" r="4" fill={`url(#${dropId})`} />
          <circle cx="124" cy="16" r="3" fill={`url(#${dropId})`} />
        </g>
        <g className="whale-body">
          <path
            d="M 280 140
               C 305 120, 320 90, 335 80
               C 340 78, 345 82, 343 90
               L 325 130
               L 343 165
               C 345 173, 340 178, 333 173
               C 318 162, 300 152, 285 155
               C 270 158, 250 168, 215 170
               C 145 174, 80 162, 50 135
               C 35 120, 30 105, 38 92
               C 50 78, 75 70, 110 70
               C 145 70, 180 78, 215 78
               C 245 78, 268 90, 280 140 Z"
            fill={`url(#${bodyId})`}
          />
          <path
            d="M 70 142
               C 95 158, 135 165, 180 165
               C 220 165, 255 162, 278 152
               C 280 158, 278 165, 270 168
               C 240 175, 195 178, 145 175
               C 105 172, 78 162, 70 142 Z"
            fill={`url(#${bellyId})`}
          />
          <circle cx="78" cy="105" r="4" fill="#0e1017" />
          <circle cx="79" cy="103" r="1.4" fill="#ffffff" opacity="0.8" />
          <path
            d="M 132 72 Q 138 64 144 72"
            fill="none"
            stroke="var(--accent-2)"
            strokeWidth="2"
            strokeLinecap="round"
            opacity="0.7"
          />
        </g>
      </svg>
    </div>
  );
}

export function UserBubble({ text }: { text: string }) {
  return (
    <div className="msg-row is-user row-end">
      <div className="msg-user">{text}</div>
    </div>
  );
}

type Segment =
  | { kind: "text"; text: string }
  | { kind: "reasoning"; text: string }
  | {
      kind: "tool";
      callId: string;
      name: string;
      args: string;
      startedAt: number;
      result?: string;
      ok?: boolean;
      durationMs?: number;
    };

export function ThinkingDots() {
  return (
    <div className="think-dots">
      <span /><span /><span />
    </div>
  );
}

export function ThinkingBar({
  startedAt,
  label,
  promptTokens,
  completionTokens,
}: {
  startedAt: number;
  label: string;
  promptTokens: number;
  completionTokens: number;
}) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 200);
    return () => window.clearInterval(id);
  }, []);
  const elapsedMs = now - startedAt;
  return (
    <div className="thinking-bar">
      <div className="thinking-bar-sweep" />
      <div className="thinking-bar-inner">
        <div className="thinking-bar-glyph">
          <Sparkles size={11} strokeWidth={2.4} />
        </div>
        <span className="thinking-bar-label">{label}</span>
        <div className="thinking-bar-orbs">
          <span /><span /><span />
        </div>
        <span className="thinking-bar-spacer" />
        <div className="thinking-bar-tokens">
          <span className="thinking-bar-token up" title={`${promptTokens.toLocaleString()} prompt tokens`}>
            <ArrowUp size={10} strokeWidth={2.6} />
            <span>{formatTokenShort(promptTokens)}</span>
          </span>
          <span
            className="thinking-bar-token down"
            title={`${completionTokens.toLocaleString()} completion tokens (live estimate)`}
          >
            <ArrowDown size={10} strokeWidth={2.6} />
            <span>{formatTokenShort(completionTokens)}</span>
          </span>
        </div>
        <span className="thinking-bar-sep">·</span>
        <span className="thinking-bar-time">{formatThinkDuration(elapsedMs)}</span>
      </div>
    </div>
  );
}

function formatTokenShort(n: number): string {
  if (n < 1000) return n.toString();
  if (n < 10_000) return `${(n / 1000).toFixed(1)}k`;
  if (n < 1_000_000) return `${Math.round(n / 1000)}k`;
  return `${(n / 1_000_000).toFixed(2)}M`;
}

function formatThinkDuration(ms: number): string {
  if (ms < 1000) return `0.${Math.floor(ms / 100)}s`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(ms < 10_000 ? 1 : 0)}s`;
  const sec = Math.floor(ms / 1000);
  return `${Math.floor(sec / 60)}m ${sec % 60}s`;
}

export function AssistantMessage({
  segments,
  pending,
}: {
  segments: Segment[];
  pending: boolean;
}) {
  const fullText = segments
    .filter((s): s is { kind: "text"; text: string } => s.kind === "text")
    .map((s) => s.text)
    .join("\n\n")
    .trim();

  const visible = segments.filter((s, i) => {
    if (s.kind === "tool") return true;
    return s.text.length > 0 || (i === segments.length - 1 && pending);
  });

  const lastSeg = segments[segments.length - 1];
  const showTailDots =
    pending && lastSeg?.kind === "tool" && lastSeg.result !== undefined;

  return (
    <div className="msg-row">
      <div className={`msg-assistant ${pending ? "streaming" : ""}`}>
        <div className="msg-role">
          <span className="msg-role-dot" />
          <span>Reasonix</span>
          {pending && <span style={{ color: "var(--accent)" }}>· streaming</span>}
          {!pending && fullText && <MessageActions text={fullText} />}
        </div>
        {visible.length === 0 && pending && <ThinkingDots />}
        {visible.map((seg, i) => {
          const isLast = i === visible.length - 1;
          if (seg.kind === "text") {
            return (
              <div key={`s${i}`}>
                {seg.text && <Markdown source={seg.text} />}
                {pending && isLast && <span className="cursor" />}
              </div>
            );
          }
          if (seg.kind === "reasoning") {
            return <Reasoning key={`r${i}`} text={seg.text} live={pending && isLast} />;
          }
          return (
            <ToolCard
              key={seg.callId}
              name={seg.name}
              args={seg.args}
              startedAt={seg.startedAt}
              result={seg.result}
              ok={seg.ok}
              durationMs={seg.durationMs}
            />
          );
        })}
        {showTailDots && <ThinkingDots />}
      </div>
    </div>
  );
}

function MessageActions({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 1400);
    } catch {
      /* ignore */
    }
  };
  return (
    <div className="msg-actions">
      <button type="button" className="icon-btn" onClick={onCopy} aria-label="copy">
        <Copy size={12} />
      </button>
      {copied && <span className="msg-actions-status">copied</span>}
    </div>
  );
}

function Reasoning({ text, live }: { text: string; live: boolean }) {
  const [open, setOpen] = useState(live);
  useEffect(() => {
    if (live) setOpen(true);
  }, [live]);
  return (
    <div className={`reasoning ${open ? "open" : ""} ${live ? "live" : ""}`}>
      <div
        className="reasoning-head"
        onClick={() => setOpen((v) => !v)}
        role="button"
        tabIndex={0}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            setOpen((v) => !v);
          }
        }}
      >
        <ChevronRight size={11} className="reasoning-chevron" />
        <span>{live ? "Thinking" : "Thought"}</span>
        <span className="reasoning-tag">{text.length}c</span>
      </div>
      <div className="reasoning-body">
        {text}
        {live && <span className="cursor" />}
      </div>
    </div>
  );
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(ms < 10_000 ? 1 : 0)}s`;
  const sec = Math.floor(ms / 1000);
  return `${Math.floor(sec / 60)}m ${sec % 60}s`;
}

function LiveDuration({ startedAt }: { startedAt: number }) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 200);
    return () => window.clearInterval(id);
  }, []);
  return <span className="tool-row-meta">{formatDuration(now - startedAt)}</span>;
}

export function ToolCard({
  name,
  args,
  startedAt,
  result,
  ok,
  durationMs,
}: {
  name: string;
  args: string;
  startedAt: number;
  result?: string;
  ok?: boolean;
  durationMs?: number;
}) {
  const [open, setOpen] = useState(false);
  const running = result === undefined;
  const preparing = running && args === "";
  const def = getToolDef(name);
  const wrapCls = [
    "tool-row-wrap",
    open ? "open" : "",
    preparing ? "preparing" : running ? "running" : "",
    ok === true ? "ok" : "",
    ok === false ? "err" : "",
  ]
    .filter(Boolean)
    .join(" ");
  const statusCls = running ? "running" : ok === false ? "err" : "ok";
  const preview = previewFor(name, args);
  const summary = summaryFor(name, args, result, ok);
  const customBody = renderToolBody(name, args, result, ok);
  return (
    <div className={wrapCls}>
        <div
          className="tool-row"
          onClick={() => setOpen((v) => !v)}
          role="button"
          tabIndex={0}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              setOpen((v) => !v);
            }
          }}
        >
          <span className={`tool-row-status ${statusCls}`}>
            {running ? (
              <Loader2 size={13} className="spin" strokeWidth={2.4} />
            ) : ok === false ? (
              <XCircle size={13} strokeWidth={2.2} />
            ) : (
              <CheckCircle2 size={13} strokeWidth={2.2} />
            )}
          </span>
          <span className={`tool-row-name k-${def.kind}`}>{name}</span>
          {preview && <span className="tool-row-preview">{preview}</span>}
          {running ? (
            <LiveDuration startedAt={startedAt} />
          ) : (
            durationMs !== undefined && (
              <span className="tool-row-meta">{formatDuration(durationMs)}</span>
            )
          )}
          {summary && (
            <span className={`tool-row-summary tone-${summary.tone}`}>{summary.text}</span>
          )}
          <ChevronRight size={12} className="tool-row-chevron" />
        </div>
        <div className="tool-row-body">
          {customBody ?? (
            <>
              {args && (
                <div className="tool-section">
                  <div className="tool-section-label">arguments</div>
                  <div className="tool-mono">{args}</div>
                </div>
              )}
              {result !== undefined && (
                <div className="tool-section">
                  <div className="tool-section-label">{ok === false ? "error" : "result"}</div>
                  <div className="tool-mono">
                    {result || <span style={{ color: "var(--text-4)" }}>(empty)</span>}
                  </div>
                </div>
              )}
            </>
          )}
        </div>
    </div>
  );
}

export function StatusLine({ text }: { text: string }) {
  return (
    <div className="msg-row row-center">
      <div className="status-line">{text}</div>
    </div>
  );
}

export function ErrorBanner({
  message,
  onRetry,
}: {
  message: string;
  onRetry?: () => void;
}) {
  return (
    <div className="msg-row row-center">
      <div className="error-banner">
        <AlertTriangle size={13} strokeWidth={2.4} />
        <span className="error-banner-text">{message}</span>
        {onRetry && (
          <button
            type="button"
            className="error-banner-retry"
            onClick={onRetry}
          >
            <RefreshCcw size={11} strokeWidth={2.4} />
            <span>Retry</span>
          </button>
        )}
      </div>
    </div>
  );
}

function firstWord(cmd: string): string {
  const m = /^\s*(\S+)/.exec(cmd);
  return m?.[1] ?? cmd;
}

export function PlanCard({
  plan,
  summary,
  onApprove,
  onRefine,
  onCancel,
}: {
  plan: string;
  summary?: string;
  onApprove: (feedback?: string) => void;
  onRefine: (feedback?: string) => void;
  onCancel: (feedback?: string) => void;
}) {
  const [feedback, setFeedback] = useState("");
  const fb = feedback.trim() || undefined;
  return (
    <div className="msg-row">
      <div className="plan-card">
        <div className="plan-card-head">
          <div className="plan-card-icon">
            <ClipboardCheck size={14} strokeWidth={2.4} />
          </div>
          <div className="plan-card-title-wrap">
            <div className="plan-card-eyebrow">PLAN PROPOSED</div>
            <div className="plan-card-title">
              {summary ?? "Reasonix proposes the following plan"}
            </div>
          </div>
        </div>
        <div className="plan-card-body">
          <Markdown source={plan} />
        </div>
        <div className="plan-card-feedback">
          <input
            type="text"
            className="choice-card-input"
            placeholder={t("modal.planFeedbackPlaceholder")}
            value={feedback}
            onChange={(e) => setFeedback(e.target.value)}
          />
        </div>
        <div className="approval-actions">
          <button type="button" className="appr-btn deny" onClick={() => onCancel(fb)}>
            <X size={13} strokeWidth={2.4} />
            <span>Cancel</span>
          </button>
          <button type="button" className="appr-btn always" onClick={() => onRefine(fb)}>
            <RefreshCcw size={13} strokeWidth={2.4} />
            <span>Refine</span>
          </button>
          <button type="button" className="appr-btn allow" onClick={() => onApprove(fb)}>
            <Check size={13} strokeWidth={2.6} />
            <span>Approve</span>
            <span className="kbd appr-btn-kbd">↵</span>
          </button>
        </div>
      </div>
    </div>
  );
}

export function ChoiceCard({
  question,
  options,
  allowCustom,
  onPick,
  onText,
  onCancel,
}: {
  question: string;
  options: { id: string; title: string; summary?: string }[];
  allowCustom: boolean;
  onPick: (optionId: string) => void;
  onText: (text: string) => void;
  onCancel: () => void;
}) {
  const [custom, setCustom] = useState("");
  return (
    <div className="msg-row">
      <div className="choice-card">
        <div className="choice-card-head">
          <div className="choice-card-icon">
            <GitBranch size={14} strokeWidth={2.4} />
          </div>
          <div className="choice-card-title-wrap">
            <div className="choice-card-eyebrow">REASONIX ASKS</div>
            <div className="choice-card-title">{question}</div>
          </div>
        </div>
        <div className="choice-card-options">
          {options.map((opt, i) => (
            <button
              key={opt.id}
              type="button"
              className="choice-card-opt"
              onClick={() => onPick(opt.id)}
            >
              <span className="choice-card-letter">{String.fromCharCode(65 + i)}</span>
              <span className="choice-card-opt-text">
                <span className="choice-card-opt-title">{opt.title}</span>
                {opt.summary && (
                  <span className="choice-card-opt-summary">{opt.summary}</span>
                )}
              </span>
              <ArrowRight size={13} className="choice-card-opt-arrow" />
            </button>
          ))}
        </div>
        {allowCustom && (
          <div className="choice-card-custom">
            <input
              type="text"
              className="choice-card-input"
              placeholder={t("modal.choiceCustomPlaceholder")}
              value={custom}
              onChange={(e) => setCustom(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && custom.trim()) {
                  e.preventDefault();
                  onText(custom.trim());
                }
              }}
            />
            <button
              type="button"
              className="appr-btn allow"
              disabled={!custom.trim()}
              onClick={() => custom.trim() && onText(custom.trim())}
            >
              <Check size={13} strokeWidth={2.6} />
              <span>Submit</span>
            </button>
          </div>
        )}
        <div className="choice-card-foot">
          <button type="button" className="choice-card-cancel" onClick={onCancel}>
            Skip this question
          </button>
        </div>
      </div>
    </div>
  );
}

export function ApprovalCard({
  kind,
  command,
  onAllow,
  onAlwaysAllow,
  onDeny,
}: {
  kind: "run_command" | "run_background";
  command: string;
  onAllow: () => void;
  onAlwaysAllow: (prefix: string) => void;
  onDeny: (reason?: string) => void;
}) {
  const prefix = firstWord(command);
  return (
    <div className="msg-row">
      <div className="approval">
        <div className="approval-head">
          <div className="approval-icon">
            <AlertTriangle size={14} strokeWidth={2.4} />
          </div>
          <div className="approval-title-wrap">
            <div className="approval-title">
              Approve {kind === "run_background" ? "background command" : "shell command"}?
            </div>
            <div className="approval-sub">
              Reasonix wants to run this. Inspect before allowing.
            </div>
          </div>
        </div>
        <div className="approval-cmd">
          <span className="approval-prompt">$</span>
          <span>{command}</span>
        </div>
        <div className="approval-actions">
          <button type="button" className="appr-btn deny" onClick={() => onDeny()}>
            <X size={13} strokeWidth={2.4} />
            <span>Deny</span>
          </button>
          <button type="button" className="appr-btn always" onClick={() => onAlwaysAllow(prefix)}>
            <ShieldCheck size={13} strokeWidth={2.4} />
            <span>Always allow</span>
            <span className="appr-btn-prefix">{prefix}</span>
          </button>
          <button type="button" className="appr-btn allow" onClick={onAllow} autoFocus>
            <Check size={13} strokeWidth={2.6} />
            <span>Allow once</span>
            <span className="kbd appr-btn-kbd">↵</span>
          </button>
        </div>
      </div>
    </div>
  );
}

export type SlashCommand = {
  id: string;
  label: string;
  hint?: string;
  run: () => void;
};

function findSlashTokenAt(
  text: string,
  caret: number,
): { start: number; end: number; query: string } | null {
  if (caret < 1 || caret > text.length) return null;
  let i = caret - 1;
  while (i >= 0) {
    const ch = text[i];
    if (ch === "/") {
      const prev = i === 0 ? "" : text[i - 1];
      if (i === 0 || prev === " " || prev === "\n" || prev === "\t") {
        return { start: i, end: caret, query: text.slice(i + 1, caret) };
      }
      return null;
    }
    if (ch === " " || ch === "\n" || ch === "\t") return null;
    i--;
  }
  return null;
}

function findAtTokenAt(
  text: string,
  caret: number,
): { start: number; end: number; query: string } | null {
  if (caret < 1 || caret > text.length) return null;
  let i = caret - 1;
  while (i >= 0) {
    const ch = text[i];
    if (ch === "@") {
      const prev = i === 0 ? "" : text[i - 1];
      if (i === 0 || prev === " " || prev === "\n" || prev === "\t") {
        return { start: i, end: caret, query: text.slice(i + 1, caret) };
      }
      return null;
    }
    if (!/[a-zA-Z0-9_.:/\\-]/.test(ch ?? "")) return null;
    i--;
  }
  return null;
}

export function Composer({
  draft,
  setDraft,
  onSend,
  onAbort,
  onOpenCommands,
  slashCommands,
  disabled,
  busy,
  textareaRef,
  onMentionQuery,
  onMentionPreview,
  onMentionPicked,
  mentionResults,
  mentionPreview,
}: {
  draft: string;
  setDraft: (v: string) => void;
  onSend: () => void;
  onAbort: () => void;
  onOpenCommands: () => void;
  slashCommands: SlashCommand[];
  disabled: boolean;
  busy: boolean;
  textareaRef?: RefObject<HTMLTextAreaElement | null>;
  onMentionQuery: (query: string, nonce: number) => void;
  onMentionPreview: (path: string, nonce: number) => void;
  onMentionPicked: (path: string) => void;
  mentionResults: { nonce: number; query: string; results: string[] } | null;
  mentionPreview:
    | { nonce: number; path: string; head: string; totalLines: number }
    | null;
}) {
  const [slashIdx, setSlashIdx] = useState(0);
  const [caret, setCaret] = useState(0);
  const [dismissedAt, setDismissedAt] = useState<number | null>(null);
  const localRef = useRef<HTMLTextAreaElement | null>(null);
  const setTextareaRef = useCallback(
    (el: HTMLTextAreaElement | null) => {
      localRef.current = el;
      if (textareaRef) textareaRef.current = el;
    },
    [textareaRef],
  );
  const slashRange = findSlashTokenAt(draft, caret);
  const slashQuery = slashRange ? slashRange.query.toLowerCase() : "";
  const filteredSlash = slashRange
    ? slashCommands.filter((c) =>
        [c.label, c.hint].filter(Boolean).join(" ").toLowerCase().includes(slashQuery),
      )
    : [];
  const slashOpen =
    slashRange !== null &&
    filteredSlash.length > 0 &&
    dismissedAt !== slashRange.start;

  const atRange = slashRange ? null : findAtTokenAt(draft, caret);
  const [mentionIdx, setMentionIdx] = useState(0);
  const [mentionDismissedAt, setMentionDismissedAt] = useState<number | null>(null);
  const mentionNonceRef = useRef(0);
  const atQuery = atRange?.query ?? null;
  const atStart = atRange?.start ?? null;
  useEffect(() => {
    if (atQuery === null || atStart === null) return;
    mentionNonceRef.current += 1;
    onMentionQuery(atQuery, mentionNonceRef.current);
  }, [atQuery, atStart, onMentionQuery]);
  const mentionList =
    atRange &&
    mentionResults &&
    mentionResults.nonce === mentionNonceRef.current &&
    mentionResults.query === atRange.query
      ? mentionResults.results
      : [];
  const mentionOpen =
    atRange !== null &&
    mentionList.length > 0 &&
    mentionDismissedAt !== atRange.start;
  const previewNonceRef = useRef(0);
  const activePath = atRange ? (mentionList[mentionIdx] ?? null) : null;
  const activeIsDir = activePath?.endsWith("/") ?? false;
  const activeBarePath = activePath
    ? activePath.includes(":")
      ? (activePath.split(":")[0] ?? activePath)
      : activePath
    : null;
  useEffect(() => {
    if (!activeBarePath || activeIsDir) return;
    previewNonceRef.current += 1;
    onMentionPreview(activeBarePath, previewNonceRef.current);
  }, [activeBarePath, activeIsDir, onMentionPreview]);
  const activePreview =
    !activeIsDir &&
    mentionPreview &&
    mentionPreview.path === activeBarePath &&
    mentionPreview.nonce === previewNonceRef.current
      ? mentionPreview
      : null;

  useEffect(() => {
    setSlashIdx(0);
  }, [slashRange?.start, slashQuery]);
  useEffect(() => {
    setMentionIdx(0);
  }, [atRange?.start, atRange?.query, mentionList.length]);
  useEffect(() => {
    if (slashRange === null && dismissedAt !== null) setDismissedAt(null);
  }, [slashRange, dismissedAt]);
  useEffect(() => {
    if (atRange === null && mentionDismissedAt !== null) setMentionDismissedAt(null);
  }, [atRange, mentionDismissedAt]);
  const replaceSlashToken = (replacement: string): { next: string; caretAt: number } => {
    if (!slashRange) return { next: draft, caretAt: caret };
    const next = draft.slice(0, slashRange.start) + replacement + draft.slice(slashRange.end);
    return { next, caretAt: slashRange.start + replacement.length };
  };
  const focusAt = (caretAt: number) => {
    const el = localRef.current;
    if (!el) return;
    requestAnimationFrame(() => {
      el.focus();
      el.setSelectionRange(caretAt, caretAt);
      setCaret(caretAt);
    });
  };
  const runSlash = (cmd: SlashCommand) => {
    const { next, caretAt } = replaceSlashToken("");
    setDraft(next);
    if (next.length === 0) {
      cmd.run();
    } else {
      focusAt(caretAt);
      cmd.run();
    }
  };
  const completeSlash = (cmd: SlashCommand) => {
    const { next, caretAt } = replaceSlashToken(`/${cmd.id}`);
    setDraft(next);
    focusAt(caretAt);
  };
  const acceptMention = (path: string) => {
    if (!atRange) return;
    const isDir = path.endsWith("/");
    const replacement = `@${path}`;
    const trail = isDir ? "" : " ";
    const next =
      draft.slice(0, atRange.start) + replacement + trail + draft.slice(atRange.end);
    setDraft(next);
    focusAt(atRange.start + replacement.length + trail.length);
    if (!isDir) onMentionPicked(path);
  };
  const onKey = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (slashOpen) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setSlashIdx((i) => Math.min(i + 1, filteredSlash.length - 1));
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setSlashIdx((i) => Math.max(i - 1, 0));
        return;
      }
      if (e.key === "Tab") {
        e.preventDefault();
        const cmd = filteredSlash[slashIdx];
        if (cmd) completeSlash(cmd);
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        if (slashRange) setDismissedAt(slashRange.start);
        return;
      }
      if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
        e.preventDefault();
        const cmd = filteredSlash[slashIdx];
        if (cmd) runSlash(cmd);
        return;
      }
    }
    if (mentionOpen) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setMentionIdx((i) => Math.min(i + 1, mentionList.length - 1));
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setMentionIdx((i) => Math.max(i - 1, 0));
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        if (atRange) setMentionDismissedAt(atRange.start);
        return;
      }
      if (
        (e.key === "Tab" || (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing))
      ) {
        e.preventDefault();
        const pick = mentionList[mentionIdx];
        if (pick) acceptMention(pick);
        return;
      }
    }
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      onSend();
    }
  };
  const syncCaret = (el: HTMLTextAreaElement) => {
    const pos = el.selectionStart ?? el.value.length;
    setCaret(pos);
  };
  const canSend = !disabled && !busy && draft.trim().length > 0;
  return (
    <div className="composer-wrap">
      {slashOpen && (
        <div className="slash-menu">
          <div className="slash-menu-head">
            <Slash size={11} />
            <span>commands</span>
            <span className="slash-menu-hint">
              <span className="kbd">↑↓</span> nav <span className="kbd">↵</span> run{" "}
              <span className="kbd">esc</span> close
            </span>
          </div>
          <div className="slash-menu-list">
            {filteredSlash.map((c, i) => (
              <button
                type="button"
                key={c.id}
                className={`slash-menu-item ${i === slashIdx ? "active" : ""}`}
                onMouseEnter={() => setSlashIdx(i)}
                onClick={() => runSlash(c)}
              >
                <span className="slash-menu-label">
                  <span className="slash-menu-name">/{c.id}</span>
                  {c.label !== c.id && (
                    <span className="slash-menu-title">{c.label}</span>
                  )}
                </span>
                {c.hint && <span className="slash-menu-item-hint">{c.hint}</span>}
              </button>
            ))}
          </div>
        </div>
      )}
      {mentionOpen && (
        <div className="slash-menu mention-menu">
          <div className="slash-menu-head">
            <AtSign size={11} />
            <span>files</span>
            <span className="slash-menu-hint">
              <span className="kbd">↑↓</span> nav <span className="kbd">↵</span> pick{" "}
              <span className="kbd">esc</span> close
            </span>
          </div>
          <div className="mention-body">
            <div className="slash-menu-list mention-list">
              {mentionList.map((p, i) => (
                <button
                  type="button"
                  key={p}
                  className={`slash-menu-item ${i === mentionIdx ? "active" : ""}`}
                  onMouseEnter={() => setMentionIdx(i)}
                  onClick={() => acceptMention(p)}
                >
                  <span className="slash-menu-label">
                    <span className="slash-menu-name mono">{p}</span>
                  </span>
                </button>
              ))}
            </div>
            {activePath && (
              <div className="mention-preview">
                <div className="mention-preview-head">
                  <span className="mono">{activePath}</span>
                  {!activeIsDir && activePreview && activePreview.totalLines > 0 && (
                    <span className="mention-preview-lines">
                      {activePreview.totalLines} L
                    </span>
                  )}
                </div>
                {activeIsDir ? (
                  <div className="mention-preview-dir">
                    directory — pick to browse contents
                  </div>
                ) : (
                  <pre className="mention-preview-body mono">
                    {activePreview
                      ? activePreview.head || "(empty file)"
                      : "loading…"}
                  </pre>
                )}
              </div>
            )}
          </div>
        </div>
      )}
      <div className="composer">
        <div className="composer-left">
          <button
            type="button"
            className="icon-btn"
            aria-label="commands"
            title="Commands (⌘K) — or type /"
            onClick={onOpenCommands}
          >
            <Slash size={14} />
          </button>
        </div>
        <textarea
          ref={setTextareaRef}
          value={draft}
          onChange={(e) => {
            setDraft(e.target.value);
            const el = e.target;
            el.style.height = "auto";
            el.style.height = `${Math.min(el.scrollHeight, 180)}px`;
            syncCaret(el);
          }}
          onSelect={(e) => syncCaret(e.currentTarget)}
          onClick={(e) => syncCaret(e.currentTarget)}
          onKeyUp={(e) => syncCaret(e.currentTarget)}
          onKeyDown={onKey}
          placeholder={busy ? t("composer.busy") : t("composer.idle")}
          disabled={disabled}
          rows={1}
        />
        <div className="composer-actions">
          {busy ? (
            <button type="button" className="send-btn stop" onClick={onAbort} aria-label="stop">
              <Square size={12} fill="currentColor" />
            </button>
          ) : (
            <button
              type="button"
              className="send-btn"
              onClick={onSend}
              disabled={!canSend}
              aria-label="send"
            >
              <ArrowUp size={16} strokeWidth={2.6} />
            </button>
          )}
        </div>
      </div>
      <div className="composer-hint">
        <div className="composer-hint-left">
          <span className="kbd-group">
            <span className="kbd">Enter</span> {t("composer.send")}
          </span>
          <span className="kbd-group">
            <span className="kbd">Shift</span>+<span className="kbd">Enter</span>{" "}
            {t("composer.newline")}
          </span>
          <span className="kbd-group">
            <span className="kbd">⌘</span>+<span className="kbd">K</span>{" "}
            {t("composer.commands")}
          </span>
        </div>
        <div className="composer-hint-right">
          {busy && <span className="streaming-pill">streaming</span>}
          {!busy && draft.length > 0 && (
            <span className="composer-count">{draft.length.toLocaleString()}</span>
          )}
        </div>
      </div>
    </div>
  );
}

export type ContextMenuAction = {
  id: string;
  label: string;
  icon: ReactNode;
  danger?: boolean;
  run: () => void;
};

export function ContextMenu({
  x,
  y,
  actions,
  onClose,
}: {
  x: number;
  y: number;
  actions: ContextMenuAction[];
  onClose: () => void;
}) {
  useEffect(() => {
    const onDocClick = () => onClose();
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("click", onDocClick);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("click", onDocClick);
      window.removeEventListener("keydown", onKey);
    };
  }, [onClose]);

  const winW = typeof window !== "undefined" ? window.innerWidth : 1024;
  const winH = typeof window !== "undefined" ? window.innerHeight : 768;
  const menuW = 180;
  const menuH = actions.length * 30 + 8;
  const left = Math.min(x, winW - menuW - 8);
  const top = Math.min(y, winH - menuH - 8);

  return (
    <div
      className="ctx-menu"
      style={{ left, top }}
      onClick={(e) => e.stopPropagation()}
      onContextMenu={(e) => e.preventDefault()}
    >
      {actions.map((a) => (
        <button
          key={a.id}
          type="button"
          className={`ctx-menu-item ${a.danger ? "danger" : ""}`}
          onClick={() => {
            a.run();
            onClose();
          }}
        >
          <span className="ctx-menu-icon">{a.icon}</span>
          <span>{a.label}</span>
        </button>
      ))}
    </div>
  );
}

export function ScrollToBottom({
  scrollerRef,
  trigger,
}: {
  scrollerRef: RefObject<HTMLDivElement | null>;
  trigger: number;
}): ReactNode {
  const [show, setShow] = useState(false);
  useEffect(() => {
    const el = scrollerRef.current;
    if (!el) return;
    const onScroll = () => {
      const slack = 80;
      const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < slack;
      setShow(!atBottom);
    };
    onScroll();
    el.addEventListener("scroll", onScroll, { passive: true });
    return () => el.removeEventListener("scroll", onScroll);
  }, [scrollerRef, trigger]);
  return (
    <button
      type="button"
      className={`scroll-fab ${show ? "show" : ""}`}
      aria-label="scroll to bottom"
      onClick={() => {
        scrollerRef.current?.scrollTo({
          top: scrollerRef.current.scrollHeight,
          behavior: "smooth",
        });
      }}
    >
      <ArrowDown size={14} />
    </button>
  );
}

type PlanStepLite = {
  id: string;
  title: string;
  action: string;
  risk?: "low" | "med" | "high";
};

function riskLabel(risk: PlanStepLite["risk"]): string {
  if (risk === "low") return t("plan.riskLow");
  if (risk === "med") return t("plan.riskMed");
  if (risk === "high") return t("plan.riskHigh");
  return "";
}

export function ActivePlanRail({
  plan,
  summary,
  steps,
  completedStepIds,
  stepResults,
  onDismiss,
}: {
  plan: string;
  summary?: string;
  steps: PlanStepLite[];
  completedStepIds: string[];
  stepResults: Record<string, string>;
  onDismiss?: () => void;
}) {
  useLang();
  const [expanded, setExpanded] = useState(false);
  const total = steps.length;
  const doneSet = new Set(completedStepIds);
  const done = doneSet.size;
  const progress = total > 0 ? t("plan.progress", { done, total }) : t("plan.progressNoTotal", { done });
  const currentIndex = total > 0 ? steps.findIndex((s) => !doneSet.has(s.id)) : -1;
  return (
    <div className="active-plan-rail">
      <button
        type="button"
        className="active-plan-head"
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
      >
        <ClipboardCheck size={13} strokeWidth={2.4} />
        <span className="active-plan-eyebrow">{t("plan.activeTitle")}</span>
        <span className="active-plan-title">{summary ?? plan.split("\n", 1)[0]}</span>
        <span className="active-plan-progress">{progress}</span>
        {total > 0 && (
          <span
            className="active-plan-bar"
            style={{ ["--pct" as unknown as string]: `${Math.round((done / total) * 100)}%` }}
            aria-hidden="true"
          />
        )}
        <ChevronRight
          size={11}
          strokeWidth={2.4}
          className="active-plan-chev"
          style={{ transform: expanded ? "rotate(90deg)" : "rotate(0deg)" }}
        />
      </button>
      {onDismiss && (
        <button
          type="button"
          className="active-plan-dismiss"
          onClick={onDismiss}
          aria-label={t("plan.dismiss")}
          title={t("plan.dismiss")}
        >
          <X size={11} strokeWidth={2.4} />
        </button>
      )}
      {expanded && (
        <div className="active-plan-body">
          {total > 0 ? (
            <ol className="active-plan-steps">
              {steps.map((s, i) => {
                const isDone = doneSet.has(s.id);
                const isCurrent = i === currentIndex;
                return (
                  <li
                    key={s.id}
                    className={`active-plan-step ${isDone ? "done" : ""} ${isCurrent ? "current" : ""}`}
                  >
                    <span className="active-plan-step-mark">
                      {isDone ? <Check size={11} strokeWidth={2.6} /> : i + 1}
                    </span>
                    <span className="active-plan-step-body">
                      <span className="active-plan-step-title">
                        {s.title}
                        {s.risk && (
                          <span className={`active-plan-step-risk r-${s.risk}`}>{riskLabel(s.risk)}</span>
                        )}
                      </span>
                      <span className="active-plan-step-action">{s.action}</span>
                      {isDone && stepResults[s.id] && (
                        <span className="active-plan-step-result">{stepResults[s.id]}</span>
                      )}
                    </span>
                  </li>
                );
              })}
            </ol>
          ) : (
            <div className="active-plan-markdown">
              <Markdown source={plan} />
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export function CheckpointCard({
  stepId,
  title,
  result,
  notes,
  completed,
  total,
  onContinue,
  onRevise,
  onStop,
}: {
  stepId: string;
  title?: string;
  result: string;
  notes?: string;
  completed: number;
  total: number;
  onContinue: () => void;
  onRevise: (feedback?: string) => void;
  onStop: () => void;
}) {
  useLang();
  const [revising, setRevising] = useState(false);
  const [feedback, setFeedback] = useState("");
  const heading = title ?? stepId;
  const progress = total > 0 ? t("checkpoint.progress", { done: completed, total }) : null;
  return (
    <div className="msg-row">
      <div className="checkpoint-card">
        <div className="checkpoint-head">
          <div className="checkpoint-mark">
            <Check size={13} strokeWidth={2.6} />
          </div>
          <div className="checkpoint-title-wrap">
            <div className="checkpoint-eyebrow">{t("checkpoint.title")}</div>
            <div className="checkpoint-title">{heading}</div>
          </div>
          {progress && <div className="checkpoint-progress">{progress}</div>}
        </div>
        <div className="checkpoint-result">{result}</div>
        {notes && (
          <div className="checkpoint-notes">
            <span className="checkpoint-notes-label">{t("checkpoint.notesLabel")}</span>
            <span className="checkpoint-notes-body">{notes}</span>
          </div>
        )}
        {revising && (
          <div className="checkpoint-revise">
            <input
              type="text"
              className="choice-card-input"
              placeholder={t("checkpoint.revisePlaceholder")}
              value={feedback}
              onChange={(e) => setFeedback(e.target.value)}
              autoFocus
            />
          </div>
        )}
        <div className="approval-actions">
          <button type="button" className="appr-btn deny" onClick={onStop}>
            <Square size={11} fill="currentColor" />
            <span>{t("checkpoint.stop")}</span>
          </button>
          {revising ? (
            <button
              type="button"
              className="appr-btn always"
              onClick={() => {
                onRevise(feedback.trim() || undefined);
                setFeedback("");
                setRevising(false);
              }}
            >
              <RefreshCcw size={13} strokeWidth={2.4} />
              <span>{t("checkpoint.sendRevise")}</span>
            </button>
          ) : (
            <button type="button" className="appr-btn always" onClick={() => setRevising(true)}>
              <RefreshCcw size={13} strokeWidth={2.4} />
              <span>{t("checkpoint.revise")}</span>
            </button>
          )}
          <button type="button" className="appr-btn allow" onClick={onContinue}>
            <Check size={13} strokeWidth={2.6} />
            <span>{t("checkpoint.continue")}</span>
            <span className="kbd appr-btn-kbd">↵</span>
          </button>
        </div>
      </div>
    </div>
  );
}

export function RevisionCard({
  reason,
  remainingSteps,
  summary,
  onAccept,
  onReject,
  onCancel,
}: {
  reason: string;
  remainingSteps: PlanStepLite[];
  summary?: string;
  onAccept: () => void;
  onReject: () => void;
  onCancel: () => void;
}) {
  useLang();
  return (
    <div className="msg-row">
      <div className="revision-card">
        <div className="revision-head">
          <div className="revision-mark">
            <RefreshCcw size={13} strokeWidth={2.4} />
          </div>
          <div className="revision-title-wrap">
            <div className="revision-eyebrow">{t("revision.title")}</div>
            <div className="revision-title">{summary ?? t("revision.subtitle")}</div>
          </div>
        </div>
        <div className="revision-reason">{reason}</div>
        <div className="revision-steps-label">{t("revision.remainingHeading")}</div>
        <ol className="revision-steps">
          {remainingSteps.map((s, i) => (
            <li key={s.id} className="revision-step">
              <span className="revision-step-mark">{i + 1}</span>
              <span className="revision-step-body">
                <span className="revision-step-title">
                  {s.title}
                  {s.risk && (
                    <span className={`active-plan-step-risk r-${s.risk}`}>{riskLabel(s.risk)}</span>
                  )}
                </span>
                <span className="revision-step-action">{s.action}</span>
              </span>
            </li>
          ))}
        </ol>
        <div className="approval-actions">
          <button type="button" className="appr-btn deny" onClick={onCancel}>
            <X size={13} strokeWidth={2.4} />
            <span>{t("revision.cancel")}</span>
          </button>
          <button type="button" className="appr-btn always" onClick={onReject}>
            <X size={13} strokeWidth={2.4} />
            <span>{t("revision.reject")}</span>
          </button>
          <button type="button" className="appr-btn allow" onClick={onAccept}>
            <Check size={13} strokeWidth={2.6} />
            <span>{t("revision.accept")}</span>
            <span className="kbd appr-btn-kbd">↵</span>
          </button>
        </div>
      </div>
    </div>
  );
}
