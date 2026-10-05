import { SseProvider } from "./sse_provider";
import type { CapabilityScope, McpCatalog, McpDraft, McpDraftServer, McpInstallResult, McpInstallScope, McpLoad, ScopeLayer } from "./port";
import { rootQuery } from "./sse_http";

// External services and the project they answer for. A draft is parsed before it
// is installed, because a paste that will not parse is the normal case while
// typing and its message is the whole feedback.
export class SseMcp extends SseProvider {
  mcp(root?: string) {
    return this.get<McpCatalog>("/mcp" + rootQuery(root));
  }
  capabilityScope() {
    return this.get<CapabilityScope>("/capability-scope");
  }
  async capabilityScopes() {
    const r = await this.get<{ scopes?: CapabilityScope[] }>("/capability-scope?all");
    return r.scopes ?? [];
  }

  // 502 carries the diagnosis in the body — the reason the retry failed is the
  // whole point of the button, so it is read rather than thrown away.
  // 502 carries the diagnosis in the body — the reason the retry failed is the
  // whole point of the button, so it is read rather than thrown away.
  async reconnectMcp(name: string) {
    const res = await fetch(this.base + "/mcp/reconnect", {
      method: "POST",
      headers: { "content-type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify({ name }),
    });
    const body = (await res.json().catch(() => ({}))) as { state?: string; tools?: number; error?: string };
    if (!res.ok && !body.error) throw new Error(`/mcp/reconnect: ${res.status}`);
    return { state: body.state ?? (res.ok ? "ready" : "failed"), tools: body.tools, error: body.error };
  }
  setMcpEnabled(name: string, enabled: boolean, scope: ScopeLayer = "project", root?: string) {
    return this.post("/mcp/enabled", { name, enabled, scope, root });
  }
  clearMcpOverride(name: string, root?: string) {
    return this.post("/mcp/enabled", { name, clear: true, scope: "project", root });
  }
  setMcpLoad(name: string, load: McpLoad) {
    return this.post("/mcp/load", { name, load });
  }

  // A parse failure is the normal case while typing, and its message is the
  // whole feedback — so it is thrown as text, not as a status code.
  // A parse failure is the normal case while typing, and its message is the
  // whole feedback — so it is thrown as text, not as a status code.
  async parseMcp(input: string): Promise<McpDraft> {
    const res = await fetch(this.base + "/mcp/parse", {
      method: "POST",
      headers: { "content-type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify({ input }),
    });
    const body = (await res.json().catch(() => ({}))) as McpDraft & { error?: string };
    if (!res.ok) throw new Error(body.error || `/mcp/parse: ${res.status}`);
    return { servers: body.servers ?? [], risks: body.risks ?? [] };
  }
  // Both go through post0 so a refusal keeps its code. Installing and removing
  // are governed moves — a server that does not allow either says so with one,
  // and rethrowing only body.error handed the reader the kernel's English.
  installMcp(server: McpDraftServer, scope: McpInstallScope): Promise<McpInstallResult> {
    return this.post0<McpInstallResult>("/mcp/install", { server, scope });
  }
  async removeMcp(name: string) {
    const body = await this.post0<{ disconnected?: boolean; stillConfigured?: boolean }>("/mcp/remove", { name });
    return { disconnected: !!body.disconnected, stillConfigured: !!body.stillConfigured };
  }
}
