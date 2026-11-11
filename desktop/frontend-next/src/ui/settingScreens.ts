import { SETTINGS } from "./prefsnav";

// Where a setting the model names is drawn, as the "section:anchor" the settings
// sheet opens on and highlights. A setting with no row here opens the sheet at
// its top; the kernel's own declaration says which ids exist.
export const SETTING_SCREEN: Record<string, string> = {
  "agent.persona.prefix": "persona:persona",
  "agent.persona.suffix": "persona:persona",
  "agent.persona.include_runtime_context": "persona:persona",
  "peers.enabled": "peers:peers",
  "peers.dispatch": "peers:peers",
  "peers.open_nested": "peers:peers",
  "peers.release_projects": "peers:peer-projects",
  "desktop.default_tool_approval_mode": "tools:approval",
  "agent.plan_approval": "session:plan-approval",
  "agent.default_mode": "common:default-mode",
  "permissions.mode": "tools:approval",
  "tools.disabled": "tools:tool-list",
  "permissions.allow": "tools:rules",
  "permissions.ask": "tools:rules",
  "permissions.deny": "tools:rules",
  "permissions.allow_dynamic_bash": "tools:rules",
  "permissions.ask_on_external_origin": "tools:external-origin",
  "sandbox.bash": "tools:sandbox",
  "sandbox.network": "tools:sandbox",
  "sandbox.allow_write": "tools:sandbox",
  "sandbox.forbid_read": "tools:sandbox",
  "tools.shell.prefer": "tools:shell",
  "tools.shell.path": "tools:shell",
  "tools.browser_tools": "tools:browser-tools",
  "tools.present_files": "display:present-files",
  "tools.web_search_backends": "tools:web-access",
  "tools.web_fetch_backends": "tools:web-access",
  "tools.searxng_url": "tools:web-access",
  "observability.otlp.enabled": "account:trace-export",
  "observability.otlp.endpoint": "account:trace-export",
  "observability.otlp.header_name": "account:trace-export",
  "observability.otlp.header_env": "account:trace-export",
  "observability.otlp.service_name": "account:trace-export",
  "tools.video_url_passthrough": "tools:web-access",
  "agent.subagent_claims": "session:subagent-claims",
  "agent.write_lease": "tools:write-lease",
  "ui.show_changed_files": "display:changed-files",
  "ui.feedback_level_notices": "display:feedback-level-notices",
  "ui.fold_turn_work": "display:fold-work",
  "agent.turn_narration": "session:turn-narration",
  "auto_archive.enabled": "storage:auto-archive",
  "auto_archive.days": "storage:auto-archive",
  "progress_watch.pause": "session:progress-watch",
  "progress_watch.rounds": "session:progress-watch",
  "progress_watch.token_multiple": "session:progress-watch",
  "progress_watch.perseveration_retries": "session:progress-watch",
  "agent.compact_ratio": "model:context",
  "default_model": "model:model",
  "providers": "providers:providers",
  "billing.display_currency": "usage:currency",
  "notifications.enabled": "common:notify",
  "notifications.turn_done": "common:notify",
  "notifications.approval_request": "common:notify",
  "notifications.ask_request": "common:notify",
  "notifications.sound": "common:notify",
  "language": "common:language",
  "desktop.language": "common:language",
  "desktop.update_mode": "common:update-mode",
  "network.proxy_mode": "network:network",
  "network.proxy_url": "network:network",
};

/** What to hand the settings sheet for a setting id: its own row, else the top. */
export const settingScreen = (id: string): string | undefined => SETTING_SCREEN[id];

export const unknownScreens = (): string[] =>
  Object.entries(SETTING_SCREEN)
    .filter(([, at]) => !SETTINGS.some((s) => `${s.section}:${s.anchor}` === at))
    .map(([id]) => id);
