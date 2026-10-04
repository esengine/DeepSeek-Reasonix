export interface Env {
  HUB: DurableObjectNamespace;
  GITHUB_WEBHOOK_SECRET?: string;
  OPS_LISTEN_TOKEN?: string;
  OPS_EMIT_TOKEN?: string;
  OPS_IGNORE_LOGINS?: string;
  OPS_CI_WORKFLOWS?: string;
}
