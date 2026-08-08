/**
 * P0 serve route map used by HttpSseHost.
 * Paths match internal/serve handler registration.
 */
export const SERVE_ROUTES = Object.freeze({
  events: { method: "GET", path: "/events" },
  history: { method: "GET", path: "/history" },
  context: { method: "GET", path: "/context" },
  status: { method: "GET", path: "/status" },
  sessions: { method: "GET", path: "/sessions" },
  skills: { method: "GET", path: "/skills" },
  todos: { method: "GET", path: "/todos" },
  checkpoints: { method: "GET", path: "/checkpoints" },
  branches: { method: "GET", path: "/branches" },
  models: { method: "GET", path: "/models" },
  providerSetup: { method: "GET", path: "/provider-setup" },
  submit: { method: "POST", path: "/submit" },
  cancel: { method: "POST", path: "/cancel" },
  approve: { method: "POST", path: "/approve" },
  answer: { method: "POST", path: "/answer" },
  plan: { method: "POST", path: "/plan" },
  compact: { method: "POST", path: "/compact" },
  newSession: { method: "POST", path: "/new" },
  rewind: { method: "POST", path: "/rewind" },
  fork: { method: "POST", path: "/fork" },
  summarize: { method: "POST", path: "/summarize" },
  toolApprovalMode: { method: "POST", path: "/tool-approval-mode" },
  autoApproveTools: { method: "POST", path: "/auto-approve-tools" },
  bypass: { method: "POST", path: "/bypass" },
  goal: { method: "POST", path: "/goal" },
  resume: { method: "POST", path: "/resume" },
  forget: { method: "POST", path: "/forget" },
  deleteSession: { method: "POST", path: "/delete-session" },
  reloadExtensions: { method: "POST", path: "/extensions/reload" },
  providerSetupSave: { method: "POST", path: "/provider-setup" },
  tabs: { method: "GET", path: "/tabs" },
  tabsCreate: { method: "POST", path: "/tabs" },
  tabsOpenProject: { method: "POST", path: "/tabs/open-project" },
});

export function tabPath(id, suffix) {
  const clean = suffix.startsWith("/") ? suffix : `/${suffix}`;
  return `/tabs/${encodeURIComponent(id)}${clean}`;
}

/** Ordered list of P0 command names for docs/tests. */
export const P0_COMMANDS = Object.freeze(Object.keys(SERVE_ROUTES));
