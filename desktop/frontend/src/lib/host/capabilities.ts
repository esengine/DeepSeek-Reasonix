/**
 * Capability bitmap for host implementations.
 * Keep in sync with electron-poc/lib/capabilities.mjs (PoC source of truth for gating).
 */

export const POC_CAPABILITIES = {
  multiTab: true,
  terminal: false,
  remote: false,
  bot: false,
  heartbeat: false,
  themePackStore: false,
  productionUpdater: false,
  steer: false,
  submitDisplay: false,
  historyPagination: false,
  workspacePicker: "shell" as const,
  singleSession: false,
  httpSse: true,
  serveBuiltinUi: true,
  providerSetup: true,
  toolApproval: true,
  planMode: true,
  goal: true,
  rewind: true,
  fork: true,
  summarize: true,
  sessions: true,
  models: true,
  todos: true,
  skills: true,
} as const;

export const WAILS_DESKTOP_CAPABILITIES = {
  multiTab: true,
  terminal: true,
  remote: true,
  bot: true,
  heartbeat: true,
  themePackStore: true,
  productionUpdater: true,
  steer: true,
  submitDisplay: true,
  historyPagination: true,
  workspacePicker: "native" as const,
  singleSession: false,
  httpSse: false,
  serveBuiltinUi: false,
  providerSetup: true,
  toolApproval: true,
  planMode: true,
  goal: true,
  rewind: true,
  fork: true,
  summarize: true,
  sessions: true,
  models: true,
  todos: true,
  skills: true,
} as const;

export function isCapabilityEnabled(
  caps: Record<string, unknown>,
  feature: string,
): boolean {
  const v = caps[feature];
  if (typeof v === "boolean") return v;
  if (typeof v === "string") return v.length > 0;
  return false;
}

/** Features present on full Wails desktop but disabled in the PoC host. */
export function unsupportedDesktopFeatures(
  caps: Record<string, unknown> = POC_CAPABILITIES,
): string[] {
  const deferred: string[] = [];
  for (const [key, full] of Object.entries(WAILS_DESKTOP_CAPABILITIES)) {
    const poc = caps[key];
    if (full === true && poc !== true) deferred.push(key);
  }
  return deferred;
}
