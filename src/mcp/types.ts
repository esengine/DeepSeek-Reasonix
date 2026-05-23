/** MCP types (spec 2024-11-05). Stdio wire format is NDJSON — one JSON-RPC message per line, no Content-Length framing. */

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

export interface JsonRpcSuccess<R = unknown> {
  jsonrpc: "2.0";
  id: JsonRpcId;
  result: R;
}

export interface JsonRpcError {
  jsonrpc: "2.0";
  id: JsonRpcId | null;
  error: {
    /** JSON-RPC standard codes: -32700 parse, -32600 invalid request, -32601 method not found, -32602 invalid params, -32603 internal. MCP also defines its own range. */
    code: number;
    message: string;
    data?: unknown;
  };
}

export type JsonRpcResponse<R = unknown> = JsonRpcSuccess<R> | JsonRpcError;

export type JsonRpcMessage = JsonRpcRequest | JsonRpcNotification | JsonRpcSuccess | JsonRpcError;

export interface McpClientInfo {
  name: string;
  version: string;
}

export interface McpClientCapabilities {
  /** Empty object advertises support without any optional sub-features. */
  tools?: Record<string, never>;
  /** Advertised when the client can consume `resources/list` + `resources/read`. */
  resources?: Record<string, never>;
  /** Advertised when the client can consume `prompts/list` + `prompts/get`. */
  prompts?: Record<string, never>;
  /** Advertised when the client can answer `roots/list`. */
  roots?: { listChanged?: boolean };
  // sampling would go here — deferred.
}

export interface McpRoot {
  uri: string;
  name?: string;
}

export interface InitializeParams {
  protocolVersion: string;
  capabilities: McpClientCapabilities;
  clientInfo: McpClientInfo;
  rootUri?: string;
  workspaceFolders?: McpRoot[];
}

export interface InitializeResult {
  protocolVersion: string;
  serverInfo: { name: string; version: string };
  capabilities: {
    tools?: { listChanged?: boolean };
    resources?: unknown;
    prompts?: unknown;
  };
  instructions?: string;
}

export interface McpToolSchema {
  /** JSON Schema — compatible with Reasonix's tools.ts JSONSchema shape. */
  type?: string;
  properties?: Record<string, unknown>;
  required?: string[];
  [extra: string]: unknown;
}

export interface McpTool {
  name: string;
  description?: string;
  /** MCP calls this `inputSchema`. Reasonix's `parameters` field is the same concept. */
  inputSchema: McpToolSchema;
}

export interface ListToolsResult {
  tools: McpTool[];
  nextCursor?: string;
}

export interface CallToolParams {
  name: string;
  arguments?: Record<string, unknown>;
  _meta?: { progressToken?: string | number };
}

export interface ProgressNotificationParams {
  progressToken: string | number;
  progress: number;
  total?: number;
  message?: string;
}

/** Values a `ProgressHandler` receives — `progressToken` is already matched away. */
export interface McpProgressInfo {
  progress: number;
  total?: number;
  message?: string;
}

export type McpProgressHandler = (info: McpProgressInfo) => void;

export interface McpContentBlockText {
  type: "text";
  text: string;
}

export interface McpContentBlockImage {
  type: "image";
  data: string;
  mimeType: string;
}

/** MCP result content is an array of typed blocks. Reasonix consumes only text for now — image blocks get stringified with a placeholder. */
export type McpContentBlock = McpContentBlockText | McpContentBlockImage;

export interface CallToolResult {
  content: McpContentBlock[];
  /** True = tool raised an error; the content describes it. */
  isError?: boolean;
}

export interface McpResource {
  uri: string;
  name: string;
  description?: string;
  /** Hint for the content type (e.g. "text/markdown"). Purely informational. */
  mimeType?: string;
}

export interface ListResourcesParams {
  /** Pagination cursor from a previous listResources response. */
  cursor?: string;
}

export interface ListResourcesResult {
  resources: McpResource[];
  nextCursor?: string;
}

export interface ReadResourceParams {
  uri: string;
}

/** Server populates exactly one of `text` (UTF-8) or `blob` (base64) per entry. */
export interface McpResourceContentsText {
  uri: string;
  mimeType?: string;
  text: string;
}

export interface McpResourceContentsBlob {
  uri: string;
  mimeType?: string;
  blob: string;
}

export type McpResourceContents = McpResourceContentsText | McpResourceContentsBlob;

export interface ReadResourceResult {
  contents: McpResourceContents[];
}

export interface McpPromptArgument {
  name: string;
  description?: string;
  required?: boolean;
}

export interface McpPrompt {
  name: string;
  description?: string;
  arguments?: McpPromptArgument[];
}

export interface ListPromptsParams {
  cursor?: string;
}

export interface ListPromptsResult {
  prompts: McpPrompt[];
  nextCursor?: string;
}

export interface GetPromptParams {
  name: string;
  arguments?: Record<string, string>;
}

export interface McpPromptMessage {
  role: "user" | "assistant";
  content: McpContentBlock | McpPromptResourceBlock;
}

export interface McpPromptResourceBlock {
  type: "resource";
  resource: McpResourceContents;
}

export interface GetPromptResult {
  description?: string;
  messages: McpPromptMessage[];
}

/** Current MCP protocol version Reasonix is coded against. */
export const MCP_PROTOCOL_VERSION = "2024-11-05";

/** Type guard — success vs error response. */
export function isJsonRpcError(msg: JsonRpcResponse): msg is JsonRpcError {
  return "error" in msg;
}
