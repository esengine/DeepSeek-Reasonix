import type { MCPAppInstanceView } from "./mcpAppProtocol";

export interface MCPAppBindings {
  MCPOpenAppInstance(server: string, tool: string, generation: number, callID: string, resourceURI: string): Promise<MCPAppInstanceView>;
  MCPOpenAppInstanceForTab(tabID: string, server: string, tool: string, generation: number, callID: string, resourceURI: string): Promise<MCPAppInstanceView>;
  MCPAppResourceDigest(instanceToken: string): Promise<string>;
  MCPAppResourceDigestForTab(tabID: string, instanceToken: string): Promise<string>;
  MCPCloseAppInstance(instanceToken: string): Promise<void>;
  MCPCloseAppInstanceForTab(tabID: string, instanceToken: string): Promise<void>;
  MCPOpenAppLink(url: string): Promise<void>;
  MCPOpenAppLinkForTab(tabID: string, instanceToken: string, url: string): Promise<void>;
  MCPAppCallTool(instanceToken: string, toolName: string, args: Record<string, unknown> | unknown): Promise<string>;
  MCPAppCallToolForTab(tabID: string, instanceToken: string, toolName: string, args: Record<string, unknown> | unknown): Promise<string>;
}

export function makeMockMCPAppBindings(): MCPAppBindings {
  return {
    async MCPOpenAppInstance() { throw new Error("MCP Apps are unavailable in browser dev mode"); },
    async MCPOpenAppInstanceForTab() { throw new Error("MCP Apps are unavailable in browser dev mode"); },
    async MCPAppResourceDigest() { return ""; },
    async MCPAppResourceDigestForTab() { return ""; },
    async MCPCloseAppInstance() {},
    async MCPCloseAppInstanceForTab() {},
    async MCPOpenAppLink(url) { window.open(url, "_blank", "noopener"); },
    async MCPOpenAppLinkForTab(_tabID, _instanceToken, url) { window.open(url, "_blank", "noopener"); },
    async MCPAppCallTool() { throw new Error("MCP Apps are unavailable in browser dev mode"); },
    async MCPAppCallToolForTab() { throw new Error("MCP Apps are unavailable in browser dev mode"); },
  };
}
