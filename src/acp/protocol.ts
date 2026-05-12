/** Wire types for the Agent Client Protocol — https://agentclientprotocol.com */

export const ACP_PROTOCOL_VERSION = 1;

export type JsonRpcId = string | number;

export interface JsonRpcRequest<P = unknown> {
  jsonrpc: "2.0";
  id: JsonRpcId;
  method: string;
  params?: P;
}

export interface JsonRpcNotification<P = unknown> {
  jsonrpc: "2.0";
  method: string;
  params?: P;
}

export interface JsonRpcError {
  code: number;
  message: string;
  data?: unknown;
}

export interface JsonRpcResponse<R = unknown> {
  jsonrpc: "2.0";
  id: JsonRpcId | null;
  result?: R;
  error?: JsonRpcError;
}

export interface InitializeParams {
  protocolVersion: number;
  clientCapabilities?: {
    fs?: { readTextFile?: boolean; writeTextFile?: boolean };
    terminal?: boolean;
  };
  clientInfo?: { name: string; title?: string; version?: string };
}

export interface InitializeResult {
  protocolVersion: number;
  agentCapabilities: {
    loadSession?: boolean;
    promptCapabilities?: { image?: boolean; audio?: boolean; embeddedContext?: boolean };
    mcpCapabilities?: { http?: boolean; sse?: boolean };
  };
  agentInfo: { name: string; title?: string; version: string };
  authMethods: never[];
}

export interface SessionNewParams {
  cwd?: string;
  mcpServers?: Array<{
    name: string;
    command?: string;
    args?: string[];
    env?: Record<string, string>;
  }>;
}

export interface SessionNewResult {
  sessionId: string;
}

export type ContentBlock =
  | { type: "text"; text: string }
  | { type: "resource"; resource: { uri: string; mimeType?: string; text?: string } }
  | { type: "image"; mimeType: string; data: string }
  | { type: "audio"; mimeType: string; data: string };

export interface SessionPromptParams {
  sessionId: string;
  prompt: ContentBlock[];
}

export type StopReason = "end_turn" | "tool_use_complete" | "cancelled" | "error";

export interface SessionPromptResult {
  stopReason: StopReason;
}

export type SessionUpdate =
  | {
      sessionUpdate: "agent_message_chunk";
      content: { type: "text"; text: string };
    }
  | {
      sessionUpdate: "agent_thought_chunk";
      content: { type: "text"; text: string };
    }
  | {
      sessionUpdate: "tool_call";
      toolCallId: string;
      title?: string;
      kind?: "read" | "edit" | "search" | "execute" | "other";
      status?: "pending" | "in_progress" | "completed" | "failed";
      rawInput?: unknown;
    }
  | {
      sessionUpdate: "tool_call_update";
      toolCallId: string;
      status?: "pending" | "in_progress" | "completed" | "failed";
      content?: Array<{ type: "content"; content: { type: "text"; text: string } }>;
    }
  | {
      sessionUpdate: "plan";
      entries: Array<{
        content: string;
        priority: "high" | "medium" | "low";
        status: "pending" | "in_progress" | "completed";
      }>;
    };

export interface SessionUpdateParams {
  sessionId: string;
  update: SessionUpdate;
}

export interface SessionCancelParams {
  sessionId: string;
}

export const ERR_PARSE = -32700;
export const ERR_INVALID_REQUEST = -32600;
export const ERR_METHOD_NOT_FOUND = -32601;
export const ERR_INVALID_PARAMS = -32602;
export const ERR_INTERNAL = -32603;

/** Extract the user prompt text out of ACP content blocks. Resource blocks contribute their inline `text` if present. */
export function flattenPrompt(blocks: ContentBlock[]): string {
  const parts: string[] = [];
  for (const b of blocks) {
    if (b.type === "text") parts.push(b.text);
    else if (b.type === "resource" && b.resource.text) parts.push(b.resource.text);
  }
  return parts.join("\n\n").trim();
}
