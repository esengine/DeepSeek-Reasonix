/**
 * Host capability bitmap for the Electron + serve PoC.
 * Unsupported desktop surfaces are gated off so UI can hide them.
 */
export const POC_CAPABILITIES = Object.freeze({
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
  workspacePicker: "shell", // dialog + open-project tab (no full serve restart)
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
});

/**
 * Full Wails desktop (for contrast / docs).
 */
export const WAILS_DESKTOP_CAPABILITIES = Object.freeze({
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
  workspacePicker: "native",
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
});

/**
 * @param {typeof POC_CAPABILITIES} caps
 * @param {string} feature
 */
export function isCapabilityEnabled(caps, feature) {
  if (!caps || typeof caps !== "object") return false;
  const v = caps[feature];
  if (typeof v === "boolean") return v;
  if (typeof v === "string") return v.length > 0;
  return false;
}

/**
 * Features that UI should hide in the PoC shell.
 * @param {typeof POC_CAPABILITIES} [caps]
 */
export function unsupportedDesktopFeatures(caps = POC_CAPABILITIES) {
  const deferred = [];
  for (const [key, full] of Object.entries(WAILS_DESKTOP_CAPABILITIES)) {
    const poc = caps[key];
    if (full === true && poc !== true) {
      deferred.push(key);
    }
  }
  return deferred;
}
