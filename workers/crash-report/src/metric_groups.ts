// Which signals the settings dashboard draws together, and in what order. A
// table rather than a query: a group is an editorial decision about what is
// read side by side, and the database has no opinion about it.

export const SETTINGS_METRIC_GROUPS: { en: string; zh: string; signals: string[] }[] = [
  {
    en: "Client",
    zh: "客户端",
    signals: ["client_surface", "client_version", "settings_language", "cli_mode", "cli_profile", "cli_permission_mode", "cli_session_mode"],
  },
  {
    // Three closed enums that answer one question: does serialising writers
    // across sessions cost anyone anything, and is the lease held longer than
    // it is used. lease_idle is the one that decides it — a high contention
    // rate with a low idle share means the lease is held exactly as long as it
    // is needed.
    en: "Workspace write lease",
    zh: "工作区写租约",
    signals: ["lease_hold", "lease_wait", "lease_idle"],
  },
  {
    en: "Appearance and layout",
    zh: "外观与布局",
    signals: [
      "settings_desktop_layout",
      "settings_theme",
      "settings_theme_style",
      "settings_display_mode",
      "settings_status_bar_style",
      "settings_status_bar_items_count",
    ],
  },
  {
    en: "Models",
    zh: "模型",
    signals: [
      "settings_default_model",
      "settings_planner_model",
      "settings_subagent_model",
      "settings_subagent_effort",
      "settings_reasoning_language",
    ],
  },
  {
    en: "Providers",
    zh: "Provider",
    signals: ["settings_provider_count", "settings_provider_access_count", "settings_provider_access"],
  },
  {
    en: "Behavior toggles",
    zh: "行为开关",
    signals: ["settings_close_behavior", "settings_check_updates"],
  },
  {
    en: "Bots",
    zh: "机器人",
    signals: [
      "settings_bot_enabled",
      "settings_bot_model",
      "settings_bot_tool_approval",
      "settings_bot_allowlist",
      "settings_bot_allow_all",
      "settings_bot_qq_enabled",
      "settings_bot_feishu_enabled",
      "settings_bot_weixin_enabled",
      "settings_bot_connection_count",
      "settings_bot_connection_provider",
      "settings_bot_connection_enabled",
      "settings_bot_connection_status",
      "settings_bot_connection_model",
      "settings_bot_connection_approval",
    ],
  },
];
