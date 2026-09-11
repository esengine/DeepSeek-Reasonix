import { canUseNativeMCPOAuth } from "../lib/mcpOAuthEligibility";
import type { ServerView } from "../lib/types";

function server(overrides: Partial<ServerView>): ServerView {
  return {
    name: "server", transport: "http", status: "failed", configured: true,
    autoStart: true, tools: 0, prompts: 0, resources: 0,
    url: "https://mcp.example.test/mcp", ...overrides,
  };
}

if (!canUseNativeMCPOAuth(server({}))) throw new Error("eligible Streamable HTTP server should offer OAuth");
if (canUseNativeMCPOAuth(server({ transport: "stdio", url: undefined }))) throw new Error("stdio must keep its retry path");
if (canUseNativeMCPOAuth(server({ transport: "sse" }))) throw new Error("legacy SSE must keep its retry path");
if (canUseNativeMCPOAuth(server({ authConfigured: true }))) throw new Error("static authentication must take precedence");
if (canUseNativeMCPOAuth(server({ url: "http://10.0.0.8/mcp" }))) throw new Error("remote HTTP must not offer OAuth");
if (!canUseNativeMCPOAuth(server({ url: "http://127.0.0.1:8787/mcp" }))) throw new Error("loopback HTTP should offer OAuth");
if (canUseNativeMCPOAuth(server({ url: "https://mcp.example.test/mcp?sig=abc" }))) throw new Error("signed URL must keep explicit auth");
if (canUseNativeMCPOAuth(server({ url: "https://user:pass@mcp.example.test/mcp" }))) throw new Error("URL userinfo must keep explicit auth");
