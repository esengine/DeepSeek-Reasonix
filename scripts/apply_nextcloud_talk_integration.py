#!/usr/bin/env python3
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]

def read(path: str) -> str:
    return (ROOT / path).read_text(encoding="utf-8")

def write(path: str, text: str) -> None:
    (ROOT / path).write_text(text, encoding="utf-8")

def replace_once(path: str, old: str, new: str) -> None:
    text = read(path)
    count = text.count(old)
    if count != 1:
        raise RuntimeError(f"{path}: expected exactly one occurrence, found {count}: {old[:100]!r}")
    write(path, text.replace(old, new, 1))

def replace_all_checked(path: str, old: str, new: str, minimum: int = 1) -> None:
    text = read(path)
    count = text.count(old)
    if count < minimum:
        raise RuntimeError(f"{path}: expected at least {minimum} occurrences, found {count}: {old[:100]!r}")
    write(path, text.replace(old, new))

def regex_once(path: str, pattern: str, repl: str, flags: int = 0) -> None:
    text = read(path)
    new, count = re.subn(pattern, repl, text, count=1, flags=flags)
    if count != 1:
        raise RuntimeError(f"{path}: regex expected one match, found {count}: {pattern[:100]!r}")
    write(path, new)

# ---------------------------------------------------------------------------
# Generic bot/config model
# ---------------------------------------------------------------------------

replace_once(
    "internal/config/config.go",
    '''type BotConnectionCredential struct {
\tAppID        string `toml:"app_id"`
\tAppSecretEnv string `toml:"app_secret_env"`
\tAccountID    string `toml:"account_id"`
\tTokenEnv     string `toml:"token_env"`
}''',
    '''type BotConnectionCredential struct {
\tAppID        string `toml:"app_id"`
\tAppSecretEnv string `toml:"app_secret_env"`
\tAccountID    string `toml:"account_id"`
\tTokenEnv     string `toml:"token_env"`
\t// Nextcloud Talk uses the generic connection record rather than a legacy
\t// top-level provider block. Secrets remain environment-variable references.
\tServerURL   string `toml:"server_url"`
\tListenAddr  string `toml:"listen_addr"`
\tWebhookPath string `toml:"webhook_path"`
\tSecretEnv   string `toml:"secret_env"`
}''',
)

replace_all_checked(
    "internal/config/config.go",
    'provider"` // qq|feishu|weixin',
    'provider"` // qq|feishu|weixin|nextcloud-talk',
)
replace_all_checked(
    "internal/config/config.go",
    'domain"`   // feishu|lark|weixin|qq',
    'domain"`   // feishu|lark|weixin|qq|nextcloud-talk',
)

replace_once(
    "internal/config/render.go",
    '''\tif cred.TokenEnv != "" {
\t\tparts["token_env"] = cred.TokenEnv
\t}
\tif len(parts) == 0 {''',
    '''\tif cred.TokenEnv != "" {
\t\tparts["token_env"] = cred.TokenEnv
\t}
\tif cred.ServerURL != "" {
\t\tparts["server_url"] = cred.ServerURL
\t}
\tif cred.ListenAddr != "" {
\t\tparts["listen_addr"] = cred.ListenAddr
\t}
\tif cred.WebhookPath != "" {
\t\tparts["webhook_path"] = cred.WebhookPath
\t}
\tif cred.SecretEnv != "" {
\t\tparts["secret_env"] = cred.SecretEnv
\t}
\tif len(parts) == 0 {''',
)

# ---------------------------------------------------------------------------
# Nextcloud Talk adapter: reusable test-send helper
# ---------------------------------------------------------------------------

regex_once(
    "internal/bot/nextcloudtalk/adapter.go",
    r'(\nfunc \(a \*adapter\) Platform\(\) bot\.Platform \{)',
    '''\n// SendText sends a plain-text message to one Nextcloud Talk conversation.
func SendText(ctx context.Context, cfg Config, chatID, text string) (bot.SendResult, error) {
\treturn New(cfg, slog.Default()).Send(ctx, bot.OutboundMessage{
\t\tChatID:   strings.TrimSpace(chatID),
\t\tChatType: bot.ChatDirect,
\t\tText:     text,
\t})
}
\\1''',
)

# ---------------------------------------------------------------------------
# Runtime binding + CLI
# ---------------------------------------------------------------------------

replace_once(
    "internal/botruntime/runtime.go",
    '''\t"reasonix/internal/bot/feishu"
\t"reasonix/internal/bot/qq"
\t"reasonix/internal/bot/weixin"''',
    '''\t"reasonix/internal/bot/feishu"
\t"reasonix/internal/bot/nextcloudtalk"
\t"reasonix/internal/bot/qq"
\t"reasonix/internal/bot/weixin"''',
)

replace_once(
    "internal/botruntime/runtime.go",
    '''\t\t\tcase bot.PlatformWeixin:
\t\t\t\tenabled[bot.PlatformWeixin] = PlatformConfigured(cfg, bot.PlatformWeixin)
\t\t\tdefault:''',
    '''\t\t\tcase bot.PlatformWeixin:
\t\t\t\tenabled[bot.PlatformWeixin] = PlatformConfigured(cfg, bot.PlatformWeixin)
\t\t\tcase bot.PlatformNextcloudTalk:
\t\t\t\tenabled[bot.PlatformNextcloudTalk] = PlatformConfigured(cfg, bot.PlatformNextcloudTalk)
\t\t\tdefault:''',
)
replace_once(
    "internal/botruntime/runtime.go",
    '''\tenabled[bot.PlatformQQ] = PlatformConfigured(cfg, bot.PlatformQQ)
\tenabled[bot.PlatformFeishu] = PlatformConfigured(cfg, bot.PlatformFeishu)
\tenabled[bot.PlatformWeixin] = PlatformConfigured(cfg, bot.PlatformWeixin)
\treturn enabled, warnings''',
    '''\tenabled[bot.PlatformQQ] = PlatformConfigured(cfg, bot.PlatformQQ)
\tenabled[bot.PlatformFeishu] = PlatformConfigured(cfg, bot.PlatformFeishu)
\tenabled[bot.PlatformWeixin] = PlatformConfigured(cfg, bot.PlatformWeixin)
\tenabled[bot.PlatformNextcloudTalk] = PlatformConfigured(cfg, bot.PlatformNextcloudTalk)
\treturn enabled, warnings''',
)
replace_once(
    "internal/botruntime/runtime.go",
    '''\t\tcase bot.PlatformQQ, bot.PlatformFeishu, bot.PlatformWeixin:
\t\tdefault:''',
    '''\t\tcase bot.PlatformQQ, bot.PlatformFeishu, bot.PlatformWeixin, bot.PlatformNextcloudTalk:
\t\tdefault:''',
)
replace_once(
    "internal/botruntime/runtime.go",
    '''\t\tcase bot.PlatformWeixin:
\t\t\tweixinCfg := cfg.Bot.Weixin
\t\t\tweixinCfg.Enabled = true
\t\t\tweixinCfg.AccountID = firstNonEmptyString(strings.TrimSpace(conn.Credential.AccountID), weixinCfg.AccountID)
\t\t\tweixinCfg.TokenEnv = firstNonEmptyString(strings.TrimSpace(conn.Credential.TokenEnv), weixinCfg.TokenEnv)
\t\t\tbindings = append(bindings, bot.AdapterBinding{ID: id, Domain: strings.TrimSpace(conn.Domain), Platform: platform, Adapter: weixin.New(weixinCfg, logger)})
\t\t\thasConnection[platform] = true
\t\t}''',
    '''\t\tcase bot.PlatformWeixin:
\t\t\tweixinCfg := cfg.Bot.Weixin
\t\t\tweixinCfg.Enabled = true
\t\t\tweixinCfg.AccountID = firstNonEmptyString(strings.TrimSpace(conn.Credential.AccountID), weixinCfg.AccountID)
\t\t\tweixinCfg.TokenEnv = firstNonEmptyString(strings.TrimSpace(conn.Credential.TokenEnv), weixinCfg.TokenEnv)
\t\t\tbindings = append(bindings, bot.AdapterBinding{ID: id, Domain: strings.TrimSpace(conn.Domain), Platform: platform, Adapter: weixin.New(weixinCfg, logger)})
\t\t\thasConnection[platform] = true
\t\tcase bot.PlatformNextcloudTalk:
\t\t\ttalkCfg := nextcloudtalk.Config{
\t\t\t\tServerURL:    strings.TrimSpace(conn.Credential.ServerURL),
\t\t\t\tListenAddr:   strings.TrimSpace(conn.Credential.ListenAddr),
\t\t\t\tWebhookPath:  strings.TrimSpace(conn.Credential.WebhookPath),
\t\t\t\tSecretEnv:    strings.TrimSpace(conn.Credential.SecretEnv),
\t\t\t\tConnectionID: id,
\t\t\t}
\t\t\tbindings = append(bindings, bot.AdapterBinding{
\t\t\t\tID: id, Domain: "nextcloud-talk", Platform: platform, Adapter: nextcloudtalk.New(talkCfg, logger),
\t\t\t})
\t\t\thasConnection[platform] = true
\t\t}''',
)

replace_all_checked(
    "internal/cli/bot.go",
    "qq,feishu,lark,weixin",
    "qq,feishu,lark,weixin,nextcloud-talk",
)
replace_once(
    "internal/cli/bot.go",
    '''\t\tstatus := "ok"
\t\tif !conn.Enabled {
\t\t\tstatus = "disabled"
\t\t} else if len(conn.SessionMappings) == 0 && (conn.Provider == string(bot.PlatformFeishu) || conn.Provider == string(bot.PlatformWeixin)) {
\t\t\tstatus = "missing"
\t\t}
\t\taddCheck("bot.connection."+id+".session_mappings", status,''',
    '''\t\tstatus := "ok"
\t\tif !conn.Enabled {
\t\t\tstatus = "disabled"
\t\t} else if len(conn.SessionMappings) == 0 && (conn.Provider == string(bot.PlatformFeishu) || conn.Provider == string(bot.PlatformWeixin) || conn.Provider == string(bot.PlatformNextcloudTalk)) {
\t\t\tstatus = "missing"
\t\t}
\t\tif conn.Provider == string(bot.PlatformNextcloudTalk) && conn.Enabled {
\t\t\tif strings.TrimSpace(conn.Credential.ServerURL) == "" {
\t\t\t\taddCheck("bot.connection."+id+".server_url", "missing", "server_url is empty")
\t\t\t} else {
\t\t\t\taddCheck("bot.connection."+id+".server_url", "ok", conn.Credential.ServerURL)
\t\t\t}
\t\t\tsecretEnv := strings.TrimSpace(conn.Credential.SecretEnv)
\t\t\tif secretEnv == "" {
\t\t\t\taddCheck("bot.connection."+id+".secret", "missing", "secret_env is empty")
\t\t\t} else if os.Getenv(secretEnv) == "" {
\t\t\t\taddCheck("bot.connection."+id+".secret", "missing", secretEnv+" is not set")
\t\t\t} else {
\t\t\t\taddCheck("bot.connection."+id+".secret", "ok", secretEnv+" is set")
\t\t\t}
\t\t}
\t\taddCheck("bot.connection."+id+".session_mappings", status,''',
)

# ---------------------------------------------------------------------------
# Desktop Go settings / connection diagnostics
# ---------------------------------------------------------------------------

replace_once(
    "desktop/bot_connection_app.go",
    '''\t"reasonix/internal/bot"
\t"reasonix/internal/bot/feishu"
\t"reasonix/internal/bot/weixin"''',
    '''\t"reasonix/internal/bot"
\t"reasonix/internal/bot/feishu"
\t"reasonix/internal/bot/nextcloudtalk"
\t"reasonix/internal/bot/weixin"''',
)

replace_once(
    "desktop/bot_connection_app.go",
    '''type BotConnectionCredentialView struct {
\tAppID        string `json:"appId"`
\tAppSecretEnv string `json:"appSecretEnv"`
\tAccountID    string `json:"accountId"`
\tTokenEnv     string `json:"tokenEnv"`
\tSecretSet    bool   `json:"secretSet"`
}''',
    '''type BotConnectionCredentialView struct {
\tAppID        string `json:"appId"`
\tAppSecretEnv string `json:"appSecretEnv"`
\tAccountID    string `json:"accountId"`
\tTokenEnv     string `json:"tokenEnv"`
\tServerURL    string `json:"serverUrl"`
\tListenAddr   string `json:"listenAddr"`
\tWebhookPath  string `json:"webhookPath"`
\tSecretEnv    string `json:"secretEnv"`
\tSecretSet    bool   `json:"secretSet"`
}''',
)

replace_once(
    "desktop/bot_connection_app.go",
    '''\t\t\t} else if conn.Credential.AppSecretEnv != "" && strings.TrimSpace(conn.Credential.AppSecretEnv) != "" && !envIsSet(conn.Credential.AppSecretEnv) {''',
    '''\t\t\t} else if conn.Provider == string(bot.PlatformNextcloudTalk) && strings.TrimSpace(conn.Credential.ServerURL) == "" {
\t\t\t\tstatus = "warning"
\t\t\t\tmessage = "Nextcloud server URL 未配置。"
\t\t\t\tphase = "config"
\t\t\t\tcode = "server_url_missing"
\t\t\t\treportable = true
\t\t\t} else if conn.Provider == string(bot.PlatformNextcloudTalk) && strings.TrimSpace(conn.Credential.SecretEnv) == "" {
\t\t\t\tstatus = "warning"
\t\t\t\tmessage = "Nextcloud Talk shared secret 环境变量未配置。"
\t\t\t\tphase = "credential"
\t\t\t\tcode = "secret_env_missing"
\t\t\t\treportable = true
\t\t\t} else if conn.Provider == string(bot.PlatformNextcloudTalk) && !envIsSet(conn.Credential.SecretEnv) {
\t\t\t\tstatus = "warning"
\t\t\t\tmessage = conn.Credential.SecretEnv + " 未设置。"
\t\t\t\tphase = "credential"
\t\t\t\tcode = "secret_missing"
\t\t\t\treportable = true
\t\t\t} else if conn.Credential.AppSecretEnv != "" && strings.TrimSpace(conn.Credential.AppSecretEnv) != "" && !envIsSet(conn.Credential.AppSecretEnv) {''',
)

replace_once(
    "desktop/bot_connection_app.go",
    '''\tif conn.Provider != "feishu" && conn.Provider != "weixin" {
\t\treturn botConnectionDiagnostic(conn, conn.ID, "warning", "send", "test_send_unsupported", "当前渠道暂不支持桌面端主动发送测试消息，可使用诊断检查基础配置。", false), nil
\t}''',
    '''\tif conn.Provider != "feishu" && conn.Provider != "weixin" && conn.Provider != string(bot.PlatformNextcloudTalk) {
\t\treturn botConnectionDiagnostic(conn, conn.ID, "warning", "send", "test_send_unsupported", "当前渠道暂不支持桌面端主动发送测试消息，可使用诊断检查基础配置。", false), nil
\t}''',
)

replace_once(
    "desktop/bot_connection_app.go",
    '''\tcase "weixin":
\t\tweixinCfg := cfg.Bot.Weixin
\t\tweixinCfg.Enabled = true
\t\tweixinCfg.AccountID = firstNonEmptyBot(conn.Credential.AccountID, weixinCfg.AccountID)
\t\tweixinCfg.TokenEnv = firstNonEmptyBot(conn.Credential.TokenEnv, weixinCfg.TokenEnv)
\t\tresult, err = weixin.SendText(ctx, weixinCfg, target, "Reasonix bot 测试消息：连接和发送链路可用。")
\t}''',
    '''\tcase "weixin":
\t\tweixinCfg := cfg.Bot.Weixin
\t\tweixinCfg.Enabled = true
\t\tweixinCfg.AccountID = firstNonEmptyBot(conn.Credential.AccountID, weixinCfg.AccountID)
\t\tweixinCfg.TokenEnv = firstNonEmptyBot(conn.Credential.TokenEnv, weixinCfg.TokenEnv)
\t\tresult, err = weixin.SendText(ctx, weixinCfg, target, "Reasonix bot 测试消息：连接和发送链路可用。")
\tcase string(bot.PlatformNextcloudTalk):
\t\tresult, err = nextcloudtalk.SendText(ctx, nextcloudtalk.Config{
\t\t\tServerURL:    strings.TrimSpace(conn.Credential.ServerURL),
\t\t\tListenAddr:   strings.TrimSpace(conn.Credential.ListenAddr),
\t\t\tWebhookPath:  strings.TrimSpace(conn.Credential.WebhookPath),
\t\t\tSecretEnv:    strings.TrimSpace(conn.Credential.SecretEnv),
\t\t\tConnectionID: conn.ID,
\t\t}, target, "Reasonix bot 测试消息：Nextcloud Talk 发送链路可用。")
\t}''',
)

replace_once(
    "desktop/bot_connection_app.go",
    '''\t\tCredential: BotConnectionCredentialView{
\t\t\tAppID: conn.Credential.AppID, AppSecretEnv: conn.Credential.AppSecretEnv, AccountID: conn.Credential.AccountID, TokenEnv: conn.Credential.TokenEnv,
\t\t\tSecretSet: botCredentialSecretSet(conn),
\t\t},''',
    '''\t\tCredential: BotConnectionCredentialView{
\t\t\tAppID: conn.Credential.AppID, AppSecretEnv: conn.Credential.AppSecretEnv, AccountID: conn.Credential.AccountID, TokenEnv: conn.Credential.TokenEnv,
\t\t\tServerURL: conn.Credential.ServerURL, ListenAddr: conn.Credential.ListenAddr, WebhookPath: conn.Credential.WebhookPath, SecretEnv: conn.Credential.SecretEnv,
\t\t\tSecretSet: botCredentialSecretSet(conn),
\t\t},''',
)

replace_once(
    "desktop/bot_connection_app.go",
    '''\tif conn.Credential.AppSecretEnv != "" {
\t\treturn envIsSet(conn.Credential.AppSecretEnv)
\t}
\tif conn.Credential.TokenEnv != "" && envIsSet(conn.Credential.TokenEnv) {''',
    '''\tif conn.Provider == string(bot.PlatformNextcloudTalk) {
\t\treturn strings.TrimSpace(conn.Credential.SecretEnv) != "" && envIsSet(conn.Credential.SecretEnv)
\t}
\tif conn.Credential.AppSecretEnv != "" {
\t\treturn envIsSet(conn.Credential.AppSecretEnv)
\t}
\tif conn.Credential.TokenEnv != "" && envIsSet(conn.Credential.TokenEnv) {''',
)

replace_once(
    "desktop/bot_connection_app.go",
    '''\t\tCredential: config.BotConnectionCredential{
\t\t\tAppID:        strings.TrimSpace(view.Credential.AppID),
\t\t\tAppSecretEnv: strings.TrimSpace(view.Credential.AppSecretEnv),
\t\t\tAccountID:    strings.TrimSpace(view.Credential.AccountID),
\t\t\tTokenEnv:     strings.TrimSpace(view.Credential.TokenEnv),
\t\t},''',
    '''\t\tCredential: config.BotConnectionCredential{
\t\t\tAppID:        strings.TrimSpace(view.Credential.AppID),
\t\t\tAppSecretEnv: strings.TrimSpace(view.Credential.AppSecretEnv),
\t\t\tAccountID:    strings.TrimSpace(view.Credential.AccountID),
\t\t\tTokenEnv:     strings.TrimSpace(view.Credential.TokenEnv),
\t\t\tServerURL:    strings.TrimRight(strings.TrimSpace(view.Credential.ServerURL), "/"),
\t\t\tListenAddr:   strings.TrimSpace(view.Credential.ListenAddr),
\t\t\tWebhookPath:  strings.TrimSpace(view.Credential.WebhookPath),
\t\t\tSecretEnv:    strings.TrimSpace(view.Credential.SecretEnv),
\t\t},''',
)

# ---------------------------------------------------------------------------
# TypeScript model
# ---------------------------------------------------------------------------

regex_once(
    "desktop/frontend/src/lib/types.ts",
    r'(export interface BotConnectionCredentialView \{\n(?:.*\n)*?\s+tokenEnv: string;\n)',
    r'''\1  serverUrl?: string;
  listenAddr?: string;
  webhookPath?: string;
  secretEnv?: string;
''',
)

replace_once(
    "desktop/frontend/src/components/SettingsPanel.tsx",
    '''      tokenEnv: String(credential.tokenEnv ?? "").trim(),
      secretSet: Boolean(credential.secretSet),''',
    '''      tokenEnv: String(credential.tokenEnv ?? "").trim(),
      serverUrl: String(credential.serverUrl ?? "").trim(),
      listenAddr: String(credential.listenAddr ?? "").trim(),
      webhookPath: String(credential.webhookPath ?? "").trim(),
      secretEnv: String(credential.secretEnv ?? "").trim(),
      secretSet: Boolean(credential.secretSet),''',
)

# ---------------------------------------------------------------------------
# Bots Settings UI
# ---------------------------------------------------------------------------

replace_once(
    "desktop/frontend/src/components/SettingsPanel.tsx",
    '''type BotInstallTarget = "qq" | "feishu" | "lark" | "weixin";
type BotOfficialInstallTarget = Exclude<BotInstallTarget, "qq">;''',
    '''type BotInstallTarget = "qq" | "feishu" | "lark" | "weixin" | "nextcloud-talk";
type BotOfficialInstallTarget = Exclude<BotInstallTarget, "qq" | "nextcloud-talk">;''',
)
replace_once(
    "desktop/frontend/src/components/SettingsPanel.tsx",
    '''const BOT_INSTALL_TARGETS: BotInstallTarget[] = ["qq", "feishu", "lark", "weixin"];
const BOT_INSTALL_DEFAULT_TIMEOUT_SECONDS = 300;
const BOT_INSTALL_MIN_POLL_SECONDS = 3;
const DEFAULT_QQ_SECRET_ENV = "QQ_BOT_APP_SECRET";
const QQ_CONNECTION_ID = "__qq_bot__";''',
    '''const BOT_INSTALL_TARGETS: BotInstallTarget[] = ["qq", "feishu", "lark", "weixin", "nextcloud-talk"];
const BOT_INSTALL_DEFAULT_TIMEOUT_SECONDS = 300;
const BOT_INSTALL_MIN_POLL_SECONDS = 3;
const DEFAULT_QQ_SECRET_ENV = "QQ_BOT_APP_SECRET";
const DEFAULT_NEXTCLOUD_TALK_SECRET_ENV = "NEXTCLOUD_TALK_BOT_SECRET";
const DEFAULT_NEXTCLOUD_TALK_LISTEN_ADDR = "127.0.0.1:38017";
const DEFAULT_NEXTCLOUD_TALK_WEBHOOK_PATH = "/reasonix/nextcloud-talk";
const NEXTCLOUD_TALK_CONNECTION_ID = "nextcloud-talk";
const QQ_CONNECTION_ID = "__qq_bot__";''',
)
replace_once(
    "desktop/frontend/src/components/SettingsPanel.tsx",
    '''function botConnectionPlatform(connection: BotConnectionView): BotPlatformKey {
  if (connection.provider === "weixin") return "weixin";
  if (connection.provider === "qq") return "qq";
  return "feishu";
}''',
    '''function botConnectionPlatform(connection: BotConnectionView): BotPlatformKey | null {
  if (connection.provider === "weixin") return "weixin";
  if (connection.provider === "qq") return "qq";
  if (connection.provider === "feishu") return "feishu";
  return null;
}''',
)

replace_once(
    "desktop/frontend/src/components/SettingsPanel.tsx",
    '''  const [qqSecretValue, setQQSecretValue] = useState("");
  const [expandedConnectionId, setExpandedConnectionId] = useState("");''',
    '''  const [qqSecretValue, setQQSecretValue] = useState("");
  const [nextcloudServerURL, setNextcloudServerURL] = useState("");
  const [nextcloudListenAddr, setNextcloudListenAddr] = useState(DEFAULT_NEXTCLOUD_TALK_LISTEN_ADDR);
  const [nextcloudWebhookPath, setNextcloudWebhookPath] = useState(DEFAULT_NEXTCLOUD_TALK_WEBHOOK_PATH);
  const [nextcloudSecretEnv, setNextcloudSecretEnv] = useState(DEFAULT_NEXTCLOUD_TALK_SECRET_ENV);
  const [nextcloudSecretValue, setNextcloudSecretValue] = useState("");
  const [expandedConnectionId, setExpandedConnectionId] = useState("");''',
)
replace_once(
    "desktop/frontend/src/components/SettingsPanel.tsx",
    '''    setQQSecretValue("");
    setTestTargets({});''',
    '''    setQQSecretValue("");
    setNextcloudSecretValue("");
    setTestTargets({});''',
)
replace_once(
    "desktop/frontend/src/components/SettingsPanel.tsx",
    '''  const isQQInstallTarget = installTarget === "qq";
  const selectedInstallLabel = botTargetLabel(installTarget, t);''',
    '''  const isQQInstallTarget = installTarget === "qq";
  const isNextcloudInstallTarget = installTarget === "nextcloud-talk";
  const selectedInstallLabel = botTargetLabel(installTarget, t);''',
)

replace_once(
    "desktop/frontend/src/components/SettingsPanel.tsx",
    '''    setDraft(nextDraft);
    setQQSecretValue("");
  };
  const removeQQBot = async () => {''',
    '''    setDraft(nextDraft);
    setQQSecretValue("");
  };
  const saveNextcloudTalkAndEnable = async () => {
    const serverURL = nextcloudServerURL.trim().replace(/\\/+$/, "");
    const listenAddr = nextcloudListenAddr.trim() || DEFAULT_NEXTCLOUD_TALK_LISTEN_ADDR;
    const webhookPath = nextcloudWebhookPath.trim() || DEFAULT_NEXTCLOUD_TALK_WEBHOOK_PATH;
    const secretEnv = nextcloudSecretEnv.trim() || DEFAULT_NEXTCLOUD_TALK_SECRET_ENV;
    const secret = nextcloudSecretValue.trim();
    if (!serverURL || !secretEnv || !secret) return;
    const now = new Date().toISOString();
    const connection = normalizeBotConnection({
      id: NEXTCLOUD_TALK_CONNECTION_ID,
      provider: "nextcloud-talk",
      domain: "nextcloud-talk",
      label: "Nextcloud Talk",
      enabled: true,
      status: "connected",
      model: "",
      toolApprovalMode: "ask",
      workspaceRoot: "",
      access: defaultBotAccess(),
      credential: {
        appId: "",
        appSecretEnv: "",
        accountId: "",
        tokenEnv: "",
        serverUrl: serverURL,
        listenAddr,
        webhookPath: webhookPath.startsWith("/") ? webhookPath : `/${webhookPath}`,
        secretEnv,
        secretSet: true,
      },
      sessionMappings: [],
      lastError: "",
      createdAt: now,
      updatedAt: now,
    });
    const nextDraft = botDraftWithDerivedGatewayState({
      ...draft,
      enabled: true,
      connections: [...draft.connections.filter((item) => item.provider !== "nextcloud-talk"), connection],
    });
    await apply(async () => {
      await app.SetBotSecret(secretEnv, secret);
      await app.SetBotSettings(nextDraft);
    });
    setDraft(nextDraft);
    setNextcloudSecretValue("");
    setExpandedConnectionId(connection.id);
  };
  const removeQQBot = async () => {''',
)

replace_once(
    "desktop/frontend/src/components/SettingsPanel.tsx",
    '''  for (const connection of draft.connections) connectedPlatforms.add(botConnectionPlatform(connection));''',
    '''  for (const connection of draft.connections) {
    const platform = botConnectionPlatform(connection);
    if (platform) connectedPlatforms.add(platform);
  }''',
)

replace_once(
    "desktop/frontend/src/components/SettingsPanel.tsx",
    '''          {(selectedConnection.provider === "feishu" || selectedConnection.provider === "weixin") ? (''',
    '''          {(selectedConnection.provider === "feishu" || selectedConnection.provider === "weixin" || selectedConnection.provider === "nextcloud-talk") ? (''',
)

replace_once(
    "desktop/frontend/src/components/SettingsPanel.tsx",
    '''          <div className="bot-credential-line">
            <span>{botConnectionCredentialSummary(selectedConnection, t)}</span>
            <strong>{selectedConnection.credential.secretSet ? t("settings.botSecretSet") : t("settings.botSecretMissing")}</strong>
          </div>
          {botConnectionSecretEnv(selectedConnection) ? (''',
    '''          <div className="bot-credential-line">
            <span>{botConnectionCredentialSummary(selectedConnection, t)}</span>
            <strong>{selectedConnection.credential.secretSet ? t("settings.botSecretSet") : t("settings.botSecretMissing")}</strong>
          </div>
          {selectedConnection.provider === "nextcloud-talk" ? (
            <div className="bot-manual-form">
              <div className="bot-card-field">
                <span>{t("settings.botNextcloudServerURL")}</span>
                <input
                  className="mem-input"
                  value={selectedConnection.credential.serverUrl ?? ""}
                  disabled={busy}
                  spellCheck={false}
                  onChange={(event) => updateConnectionCredential(selectedConnection.id, { serverUrl: event.target.value })}
                  onBlur={(event) => void persistConnectionCredential(selectedConnection.id, { serverUrl: event.currentTarget.value })}
                />
              </div>
              <div className="bot-card-field">
                <span>{t("settings.botNextcloudListenAddr")}</span>
                <input
                  className="mem-input"
                  value={selectedConnection.credential.listenAddr ?? ""}
                  disabled={busy}
                  spellCheck={false}
                  onChange={(event) => updateConnectionCredential(selectedConnection.id, { listenAddr: event.target.value })}
                  onBlur={(event) => void persistConnectionCredential(selectedConnection.id, { listenAddr: event.currentTarget.value })}
                />
              </div>
              <div className="bot-card-field">
                <span>{t("settings.botNextcloudWebhookPath")}</span>
                <input
                  className="mem-input"
                  value={selectedConnection.credential.webhookPath ?? ""}
                  disabled={busy}
                  spellCheck={false}
                  onChange={(event) => updateConnectionCredential(selectedConnection.id, { webhookPath: event.target.value })}
                  onBlur={(event) => void persistConnectionCredential(selectedConnection.id, { webhookPath: event.currentTarget.value })}
                />
              </div>
            </div>
          ) : null}
          {botConnectionSecretEnv(selectedConnection) ? (''',
)

replace_once(
    "desktop/frontend/src/components/SettingsPanel.tsx",
    '''      ) : (
        <div className="bot-connect-panel bot-connect-panel--phone">''',
    '''      ) : isNextcloudInstallTarget ? (
        <div className="bot-connect-panel bot-connect-panel--manual">
          <div className="bot-connect-panel__body">
            <div className="bot-qq-simple__head">
              <div>
                <strong>{selectedInstallLabel}</strong>
                <p>{t("settings.botInstallManualNextcloud")}</p>
              </div>
              <span className="bot-qq-simple__status">
                <KeyRound aria-hidden="true" />
                {t("settings.botInstallNextcloudHint")}
              </span>
            </div>
            <div className="bot-manual-form">
              <div className="bot-card-field">
                <span>{t("settings.botNextcloudServerURL")}</span>
                <input
                  className="mem-input"
                  value={nextcloudServerURL}
                  disabled={busy}
                  placeholder="https://cloud.example.com"
                  spellCheck={false}
                  onChange={(event) => setNextcloudServerURL(event.target.value)}
                />
              </div>
              <div className="bot-card-field">
                <span>{t("settings.botNextcloudListenAddr")}</span>
                <input
                  className="mem-input"
                  value={nextcloudListenAddr}
                  disabled={busy}
                  placeholder={DEFAULT_NEXTCLOUD_TALK_LISTEN_ADDR}
                  spellCheck={false}
                  onChange={(event) => setNextcloudListenAddr(event.target.value)}
                />
              </div>
              <div className="bot-card-field">
                <span>{t("settings.botNextcloudWebhookPath")}</span>
                <input
                  className="mem-input"
                  value={nextcloudWebhookPath}
                  disabled={busy}
                  placeholder={DEFAULT_NEXTCLOUD_TALK_WEBHOOK_PATH}
                  spellCheck={false}
                  onChange={(event) => setNextcloudWebhookPath(event.target.value)}
                />
              </div>
              <div className="bot-card-field">
                <span>{t("settings.botNextcloudSecretEnv")}</span>
                <input
                  className="mem-input"
                  value={nextcloudSecretEnv}
                  disabled={busy}
                  placeholder={DEFAULT_NEXTCLOUD_TALK_SECRET_ENV}
                  spellCheck={false}
                  onChange={(event) => setNextcloudSecretEnv(event.target.value)}
                />
              </div>
              <div className="bot-card-field">
                <span>{t("settings.botAppSecret")}</span>
                <input
                  className="mem-input"
                  type="password"
                  value={nextcloudSecretValue}
                  disabled={busy}
                  placeholder={t("settings.botSecretPaste")}
                  spellCheck={false}
                  onChange={(event) => setNextcloudSecretValue(event.target.value)}
                />
              </div>
              <div className="bot-qq-simple__actions">
                <button
                  type="button"
                  className="btn btn--primary btn--small"
                  disabled={busy || !nextcloudServerURL.trim() || !nextcloudSecretEnv.trim() || !nextcloudSecretValue.trim()}
                  onClick={() => void saveNextcloudTalkAndEnable()}
                >
                  {t("settings.botSaveAndEnable")}
                </button>
              </div>
              <div className="bot-connect-panel__hint">{t("settings.botNextcloudSetupHelp")}</div>
            </div>
          </div>
        </div>
      ) : (
        <div className="bot-connect-panel bot-connect-panel--phone">''',
)

replace_once(
    "desktop/frontend/src/components/SettingsPanel.tsx",
    '''onClick={() => void startInstall(installTarget)}''',
    '''onClick={() => void startInstall(installTarget as BotOfficialInstallTarget)}''',
)

replace_once(
    "desktop/frontend/src/components/SettingsPanel.tsx",
    '''                            <option value="weixin">{t("settings.botWeixin")}</option>''',
    '''                            <option value="weixin">{t("settings.botWeixin")}</option>
                            <option value="nextcloud-talk">{t("settings.botNextcloudTalk")}</option>''',
)

replace_once(
    "desktop/frontend/src/components/SettingsPanel.tsx",
    '''    case "weixin": return t("settings.botWeixin");
    default: return t("settings.botFeishu");''',
    '''    case "weixin": return t("settings.botWeixin");
    case "nextcloud-talk": return t("settings.botNextcloudTalk");
    default: return t("settings.botFeishu");''',
)
replace_once(
    "desktop/frontend/src/components/SettingsPanel.tsx",
    '''    case "weixin": return t("settings.botInstallWeixinHint");
    default: return t("settings.botInstallFeishuHint");''',
    '''    case "weixin": return t("settings.botInstallWeixinHint");
    case "nextcloud-talk": return t("settings.botInstallNextcloudHint");
    default: return t("settings.botInstallFeishuHint");''',
)
replace_once(
    "desktop/frontend/src/components/SettingsPanel.tsx",
    '''function botInstallTargetMatchesConnection(target: BotOfficialInstallTarget, connection: BotConnectionView): boolean {
  if (target === "weixin") return connection.provider === "weixin";
  if (target === "lark") return connection.provider === "feishu" && connection.domain === "lark";
  return connection.provider === "feishu" && connection.domain !== "lark";
}''',
    '''function botInstallTargetMatchesConnection(target: BotInstallTarget, connection: BotConnectionView): boolean {
  if (target === "qq") return connection.provider === "qq";
  if (target === "weixin") return connection.provider === "weixin";
  if (target === "nextcloud-talk") return connection.provider === "nextcloud-talk";
  if (target === "lark") return connection.provider === "feishu" && connection.domain === "lark";
  return connection.provider === "feishu" && connection.domain !== "lark";
}''',
)
replace_once(
    "desktop/frontend/src/components/SettingsPanel.tsx",
    '''  if (connection.provider === "qq") return "qq";
  return "feishu";''',
    '''  if (connection.provider === "qq") return "qq";
  if (connection.provider === "nextcloud-talk") return "nextcloud-talk";
  return "feishu";''',
)
replace_once(
    "desktop/frontend/src/components/SettingsPanel.tsx",
    '''  if (connection.provider === "qq") return "QQ";
  return t("settings.botFeishu");''',
    '''  if (connection.provider === "qq") return "QQ";
  if (connection.provider === "nextcloud-talk") return t("settings.botNextcloudTalk");
  return t("settings.botFeishu");''',
)
replace_once(
    "desktop/frontend/src/components/SettingsPanel.tsx",
    '''function botConnectionSecretEnv(connection: BotConnectionView): string {
  return connection.provider === "weixin" ? connection.credential.tokenEnv : connection.credential.appSecretEnv;
}

function botConnectionSecretPatch(connection: BotConnectionView, value: string): Partial<BotConnectionView["credential"]> {
  return connection.provider === "weixin" ? { tokenEnv: value } : { appSecretEnv: value };
}''',
    '''function botConnectionSecretEnv(connection: BotConnectionView): string {
  if (connection.provider === "weixin") return connection.credential.tokenEnv;
  if (connection.provider === "nextcloud-talk") return connection.credential.secretEnv ?? "";
  return connection.credential.appSecretEnv;
}

function botConnectionSecretPatch(connection: BotConnectionView, value: string): Partial<BotConnectionView["credential"]> {
  if (connection.provider === "weixin") return { tokenEnv: value };
  if (connection.provider === "nextcloud-talk") return { secretEnv: value };
  return { appSecretEnv: value };
}''',
)
replace_once(
    "desktop/frontend/src/components/SettingsPanel.tsx",
    '''  if (connection.credential.appId) {
    return t("settings.botCredentialApp", { value: connection.credential.appId });
  }''',
    '''  if (connection.provider === "nextcloud-talk") {
    return connection.credential.serverUrl || t("settings.botCredentialConfigured");
  }
  if (connection.credential.appId) {
    return t("settings.botCredentialApp", { value: connection.credential.appId });
  }''',
)

# ---------------------------------------------------------------------------
# Sidebar / channel navigation
# ---------------------------------------------------------------------------

replace_once(
    "desktop/frontend/src/App.tsx",
    '''type SidebarImPlatform = "qq" | "feishu" | "lark" | "weixin";''',
    '''type SidebarImPlatform = "qq" | "feishu" | "lark" | "weixin" | "nextcloud-talk";''',
)
replace_once(
    "desktop/frontend/src/App.tsx",
    '''function isSidebarImConnection(connection: BotConnectionView): boolean {
  return connection.provider === "feishu" || connection.provider === "weixin";
}

function sidebarImPlatform(connection: BotConnectionView): SidebarImPlatform {
  if (connection.provider === "weixin") return "weixin";
  return connection.domain === "lark" ? "lark" : "feishu";
}''',
    '''function isSidebarImConnection(connection: BotConnectionView): boolean {
  return connection.provider === "feishu" || connection.provider === "weixin" || connection.provider === "nextcloud-talk";
}

function sidebarImPlatform(connection: BotConnectionView): SidebarImPlatform {
  if (connection.provider === "weixin") return "weixin";
  if (connection.provider === "nextcloud-talk") return "nextcloud-talk";
  return connection.domain === "lark" ? "lark" : "feishu";
}''',
)
replace_once(
    "desktop/frontend/src/App.tsx",
    '''  if (platform === "weixin") return translate("settings.botWeixin");
  return translate("settings.botFeishu");''',
    '''  if (platform === "weixin") return translate("settings.botWeixin");
  if (platform === "nextcloud-talk") return translate("settings.botNextcloudTalk");
  return translate("settings.botFeishu");''',
)
replace_once(
    "desktop/frontend/src/App.tsx",
    '''  if (platform === "weixin") return uniqueTrimmedValues(asArray(bot.allowlist.weixinUsers));
  return uniqueTrimmedValues(asArray(bot.allowlist.feishuUsers));''',
    '''  if (platform === "weixin") return uniqueTrimmedValues(asArray(bot.allowlist.weixinUsers));
  if (platform === "nextcloud-talk") return [];
  return uniqueTrimmedValues(asArray(bot.allowlist.feishuUsers));''',
)
replace_once(
    "desktop/frontend/src/App.tsx",
    '''      const allowlistUsers = sidebarImAllowlistUsers(bot, platform);
      const identityLabel = botMappingIdentityLabel(mapping);''',
    '''      const allowlistUsers = platform === "nextcloud-talk"
        ? uniqueTrimmedValues(asArray(connection.access.users))
        : sidebarImAllowlistUsers(bot, platform);
      const identityLabel = botMappingIdentityLabel(mapping);''',
)
replace_once(
    "desktop/frontend/src/App.tsx",
    '''        allowAll: bot.allowlist.allowAll,
        allowlistEnabled: bot.allowlist.enabled,
        allowlistUsers,''',
    '''        allowAll: platform === "nextcloud-talk" ? connection.access.allowAll : bot.allowlist.allowAll,
        allowlistEnabled: platform === "nextcloud-talk" ? connection.access.enabled : bot.allowlist.enabled,
        allowlistUsers,''',
)
replace_once(
    "desktop/frontend/src/App.tsx",
    '''          {connection.platform === "qq" ? "Q" : connection.platform === "weixin" ? "微" : connection.platform === "lark" ? "L" : "飞"}''',
    '''          {connection.platform === "qq" ? "Q" : connection.platform === "weixin" ? "微" : connection.platform === "nextcloud-talk" ? "N" : connection.platform === "lark" ? "L" : "飞"}''',
)

# ---------------------------------------------------------------------------
# Locales
# ---------------------------------------------------------------------------

locale_additions = {
    "desktop/frontend/src/locales/en.ts": '''  "settings.botNextcloudTalk": "Nextcloud Talk",
  "settings.botInstallNextcloudHint": "Self-hosted Talk bot via signed webhooks.",
  "settings.botInstallManualNextcloud": "Enter your Nextcloud server and webhook settings. Use the same shared secret when registering the Talk bot.",
  "settings.botNextcloudServerURL": "Nextcloud server URL",
  "settings.botNextcloudListenAddr": "Webhook listen address",
  "settings.botNextcloudWebhookPath": "Webhook path",
  "settings.botNextcloudSecretEnv": "Shared secret environment variable",
  "settings.botNextcloudSetupHelp": "Register the bot with occ talk:bot:install using this webhook path and shared secret. If Nextcloud runs elsewhere, expose this listener through an HTTPS reverse proxy.",
''',
    "desktop/frontend/src/locales/zh.ts": '''  "settings.botNextcloudTalk": "Nextcloud Talk",
  "settings.botInstallNextcloudHint": "通过签名 Webhook 连接自托管 Talk Bot。",
  "settings.botInstallManualNextcloud": "填写 Nextcloud 服务器和 Webhook 设置。注册 Talk Bot 时请使用相同的共享密钥。",
  "settings.botNextcloudServerURL": "Nextcloud 服务器 URL",
  "settings.botNextcloudListenAddr": "Webhook 监听地址",
  "settings.botNextcloudWebhookPath": "Webhook 路径",
  "settings.botNextcloudSecretEnv": "共享密钥环境变量",
  "settings.botNextcloudSetupHelp": "使用相同的 Webhook 路径和共享密钥执行 occ talk:bot:install。若 Nextcloud 位于其他主机，请通过 HTTPS 反向代理暴露此监听端点。",
''',
    "desktop/frontend/src/locales/zh-TW.ts": '''  "settings.botNextcloudTalk": "Nextcloud Talk",
  "settings.botInstallNextcloudHint": "透過簽名 Webhook 連接自架 Talk Bot。",
  "settings.botInstallManualNextcloud": "填寫 Nextcloud 伺服器與 Webhook 設定。註冊 Talk Bot 時請使用相同的共享密鑰。",
  "settings.botNextcloudServerURL": "Nextcloud 伺服器 URL",
  "settings.botNextcloudListenAddr": "Webhook 監聽位址",
  "settings.botNextcloudWebhookPath": "Webhook 路徑",
  "settings.botNextcloudSecretEnv": "共享密鑰環境變數",
  "settings.botNextcloudSetupHelp": "使用相同的 Webhook 路徑與共享密鑰執行 occ talk:bot:install。若 Nextcloud 位於其他主機，請透過 HTTPS 反向代理公開此監聽端點。",
''',
}
for path, additions in locale_additions.items():
    text = read(path)
    if '"settings.botNextcloudTalk"' in text:
        raise RuntimeError(f"{path}: Nextcloud locale keys already exist")
    pattern = r'(^\s*"settings\.botInstallWeixinHint":.*\n)'
    new, count = re.subn(pattern, lambda m: m.group(1) + additions, text, count=1, flags=re.M)
    if count != 1:
        raise RuntimeError(f"{path}: could not find botInstallWeixinHint anchor")
    if path.endswith("/en.ts"):
        new = new.replace(
            '"sidebar.imEmpty": "Connect QQ / Feishu / Lark / WeChat"',
            '"sidebar.imEmpty": "Connect QQ / Feishu / Lark / WeChat / Nextcloud Talk"',
        )
        new = new.replace(
            '"settings.pageDesc.bots": "Configure QQ, Feishu, Lark, and WeChat bot channels, including each bot\'s model and runtime parameters."',
            '"settings.pageDesc.bots": "Configure QQ, Feishu, Lark, WeChat, and Nextcloud Talk bot channels, including each bot\'s model and runtime parameters."',
        )
        new = new.replace(
            '"settings.botChannelsHint": "Pick a channel; QQ uses manual setup, while Feishu, Lark, and WeChat connect by QR."',
            '"settings.botChannelsHint": "Pick a channel; QQ and Nextcloud Talk use manual setup, while Feishu, Lark, and WeChat connect by QR."',
        )
    write(path, new)

# ---------------------------------------------------------------------------
# Documentation
# ---------------------------------------------------------------------------

guide = read("docs/BOT_GUIDE.md")
guide = guide.replace(
    "connect Feishu, Lark,\\nWeChat, and QQ bots",
    "connect Feishu, Lark,\\nWeChat, QQ, and Nextcloud Talk bots",
)
guide = guide.replace(
    "from Feishu, Lark,\\nWeChat, or QQ.",
    "from Feishu, Lark,\\nWeChat, QQ, or Nextcloud Talk.",
)
guide = guide.replace(
    "## Connect the four channels",
    "## Connect the channels",
)
guide = guide.replace(
    "- [Connect the four channels](#connect-the-four-channels)",
    "- [Connect the channels](#connect-the-channels)",
)
guide = guide.replace(
    "reasonix bot start --channels qq,feishu,lark,weixin --dir /path/to/project",
    "reasonix bot start --channels qq,feishu,lark,weixin,nextcloud-talk --dir /path/to/project",
)
if "### Nextcloud Talk" not in guide:
    anchor = "## Run the bot headlessly"
    if anchor not in guide:
        raise RuntimeError("docs/BOT_GUIDE.md: headless anchor not found")
    section = r'''### Nextcloud Talk

Nextcloud Talk uses its official signed bot webhook API. In **Settings -> Bots**,
choose **Nextcloud Talk** and enter:

1. The Nextcloud server URL, for example `https://cloud.example.com`.
2. The local webhook listen address (default `127.0.0.1:38017`).
3. The webhook path (default `/reasonix/nextcloud-talk`).
4. An environment-variable name for the shared secret and the secret value.

Register the webhook bot on the Nextcloud server with the same shared secret and
the externally reachable callback URL:

```sh
php occ talk:bot:install -- "Reasonix" "CHANGE_ME_SHARED_SECRET" \
  "https://reasonix.example.com/reasonix/nextcloud-talk" "Reasonix bot"
```

If Nextcloud cannot reach the Reasonix machine directly, place the loopback
listener behind an HTTPS reverse proxy. Incoming webhook requests are rejected
unless their Talk HMAC signature verifies. Outbound messages use the same shared
secret and Talk's signed bot-message endpoint.

Nextcloud Talk uses Reasonix's normal per-connection access settings, routing,
session mappings, text approvals, and `/answer` command flow.

'''
    guide = guide.replace(anchor, section + anchor)
write("docs/BOT_GUIDE.md", guide)

doc = read("docs/NEXTCLOUD_TALK_BOT.md")
if "## Desktop setup" not in doc:
    doc += r'''

## Desktop setup

Reasonix exposes Nextcloud Talk under **Settings -> Bots -> Nextcloud Talk**.
The desktop form stores the server URL, listener and webhook path in the normal
`[[bot.connections]]` record, while the shared secret is stored through the
Reasonix secret environment store.

The same connection can be used by the headless gateway:

```sh
reasonix bot start --channels nextcloud-talk --dir /path/to/project
```
'''
write("docs/NEXTCLOUD_TALK_BOT.md", doc)

# ---------------------------------------------------------------------------
# Focused tests
# ---------------------------------------------------------------------------

write(
    "internal/botruntime/nextcloud_talk_test.go",
    r'''package botruntime

import (
\t"log/slog"
\t"testing"

\t"reasonix/internal/bot"
\t"reasonix/internal/config"
)

func TestNextcloudTalkConnectionIsEnabledAndBound(t *testing.T) {
\tcfg := config.Default()
\tcfg.Bot.Connections = []config.BotConnectionConfig{{
\t\tID:       "nextcloud-talk",
\t\tProvider: string(bot.PlatformNextcloudTalk),
\t\tDomain:   "nextcloud-talk",
\t\tEnabled:  true,
\t\tCredential: config.BotConnectionCredential{
\t\t\tServerURL:   "https://cloud.example.com",
\t\t\tListenAddr:  "127.0.0.1:38017",
\t\t\tWebhookPath: "/reasonix/nextcloud-talk",
\t\t\tSecretEnv:   "NEXTCLOUD_TALK_BOT_SECRET",
\t\t},
\t}}

\tenabled, warnings := EnabledPlatforms(cfg, []string{"nextcloud-talk"})
\tif len(warnings) != 0 {
\t\tt.Fatalf("warnings = %v", warnings)
\t}
\tif !enabled[bot.PlatformNextcloudTalk] {
\t\tt.Fatal("nextcloud-talk was not enabled")
\t}

\tbindings := AdapterBindings(cfg, enabled, nil, slog.Default())
\tif len(bindings) != 1 {
\t\tt.Fatalf("bindings = %d, want 1", len(bindings))
\t}
\tif bindings[0].Platform != bot.PlatformNextcloudTalk {
\t\tt.Fatalf("platform = %q", bindings[0].Platform)
\t}
\tif bindings[0].ID != "nextcloud-talk" {
\t\tt.Fatalf("id = %q", bindings[0].ID)
\t}
\tif got := bindings[0].Adapter.Name(); got != "nextcloud-talk" {
\t\tt.Fatalf("adapter name = %q", got)
\t}
}
''',
)

write(
    "internal/config/nextcloud_talk_bot_test.go",
    r'''package config

import (
\t"strings"
\t"testing"
)

func TestRenderBotCredentialNextcloudTalk(t *testing.T) {
\tgot := renderBotCredential(BotConnectionCredential{
\t\tServerURL:   "https://cloud.example.com",
\t\tListenAddr:  "127.0.0.1:38017",
\t\tWebhookPath: "/reasonix/nextcloud-talk",
\t\tSecretEnv:   "NEXTCLOUD_TALK_BOT_SECRET",
\t})
\tfor _, want := range []string{
\t\t`server_url = "https://cloud.example.com"`,
\t\t`listen_addr = "127.0.0.1:38017"`,
\t\t`webhook_path = "/reasonix/nextcloud-talk"`,
\t\t`secret_env = "NEXTCLOUD_TALK_BOT_SECRET"`,
\t} {
\t\tif !strings.Contains(got, want) {
\t\t\tt.Fatalf("renderBotCredential() = %q, missing %q", got, want)
\t\t}
\t}
}
''',
)

write(
    "desktop/nextcloud_talk_bot_test.go",
    r'''package main

import (
\t"testing"

\t"reasonix/internal/config"
)

func TestNextcloudTalkConnectionCredentialRoundTrip(t *testing.T) {
\tt.Setenv("NEXTCLOUD_TALK_BOT_SECRET", "secret")
\tview := BotConnectionView{
\t\tID:       "nextcloud-talk",
\t\tProvider: "nextcloud-talk",
\t\tDomain:   "nextcloud-talk",
\t\tEnabled:  true,
\t\tCredential: BotConnectionCredentialView{
\t\t\tServerURL:   "https://cloud.example.com",
\t\t\tListenAddr:  "127.0.0.1:38017",
\t\t\tWebhookPath: "/reasonix/nextcloud-talk",
\t\t\tSecretEnv:   "NEXTCLOUD_TALK_BOT_SECRET",
\t\t},
\t}
\tconfigs := botConnectionConfigs([]BotConnectionView{view})
\tif len(configs) != 1 {
\t\tt.Fatalf("configs = %d, want 1", len(configs))
\t}
\tcred := configs[0].Credential
\tif cred.ServerURL != "https://cloud.example.com" || cred.ListenAddr != "127.0.0.1:38017" ||
\t\tcred.WebhookPath != "/reasonix/nextcloud-talk" || cred.SecretEnv != "NEXTCLOUD_TALK_BOT_SECRET" {
\t\tt.Fatalf("credential = %+v", cred)
\t}
\tif !botCredentialSecretSet(config.BotConnectionConfig{
\t\tProvider:   "nextcloud-talk",
\t\tCredential: cred,
\t}) {
\t\tt.Fatal("expected Nextcloud Talk secret to be detected")
\t}
}
''',
)

print("Nextcloud Talk integration changes applied.")