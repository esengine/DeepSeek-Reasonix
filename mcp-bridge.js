#!/usr/bin/env node
/**
 * mcp-bridge.js — Streamable HTTP MCP 桥接脚本 (v4.1.1)
 *
 * 一个通用的 MCP 协议桥接工具，支持两种运行模式：
 *
 * 🖥️ CLI 模式（默认）：
 *   将 streamable-http MCP 服务通过 CLI 命令暴露给 shell 环境使用。
 *   适用于不支持 SSE 长连接的 AI 代理。
 *
 * 🧩 Server 模式（--server）：
 *   作为一个标准的 stdio MCP Server 运行，内部自动代理到
 *   streamable-http MCP 服务。任何支持 MCP 的客户端
 *   （Claude Desktop、VS Code、Cursor 等）都可以直接配置使用。
 *
 * 环境变量：
 *   MCP_SERVER_URL    - 后端 MCP 服务地址（默认 http://127.0.0.1:12306/mcp-new）
 *   MCP_SERVER_ORIGIN - 发往后端的 Origin（默认 http://127.0.0.1）
 *   CHROME_MCP_API_KEY - 可选的后端 API Key（转发为 Bearer）
 *   MCP_PROTOCOL_MODE - auto（默认）、stateless 或 legacy
 *   DEBUG             - 设为 1 开启详细日志
 */

'use strict';

const path = require('path');
const os = require('os');
const fs = require('fs');

// ── 配置 ──────────────────────────────────────────────────────────────────

const MCP_URL = process.env.MCP_SERVER_URL || 'http://127.0.0.1:12306/mcp-new';
const MCP_ORIGIN = process.env.MCP_SERVER_ORIGIN || 'http://127.0.0.1';
const CHROME_MCP_API_KEY = process.env.CHROME_MCP_API_KEY?.trim();
const MCP_PROTOCOL_MODE = (process.env.MCP_PROTOCOL_MODE || 'auto').trim().toLowerCase();
const MCP_SERVER_PATH = (() => {
  try { return new URL(MCP_URL).pathname.replace(/\/$/, '') || '/'; } catch { return ''; }
})();
const USE_STATELESS_PROTOCOL = MCP_PROTOCOL_MODE === 'stateless'
  || (MCP_PROTOCOL_MODE !== 'legacy' && MCP_SERVER_PATH === '/mcp-new');
const MCP_PROTOCOL_VERSION = process.env.MCP_PROTOCOL_VERSION
  || (USE_STATELESS_PROTOCOL ? '2026-07-28' : '2025-11-25');
const DEFAULT_TOOL_TIMEOUT_MS = 60_000;
const LONG_TOOL_TIMEOUT_MS = 120_000;
const MIN_TOOL_TRANSPORT_TIMEOUT_MS = 20_000;
const LONG_TOOL = /(?:performance|trace|record|download|upload|proxy_diagnostics|collect_virtual_list)/;
const MAX_RETRIES = 2;
const STDIN_TIMEOUT_MS = 3000;
const SESSION_FILE = path.join(os.tmpdir(), 'mcp-bridge-session.json');
const SERVER_NAME = 'mcp-bridge-server';
const SERVER_VERSION = '4.1.1';
const BACKEND_CLIENT_NAME = 'mcp-bridge-backend';
const BACKEND_CLIENT_VERSION = '4.1.1';

// MCP 协议版本协商 — 与服务端 SDK 列表保持一致
const SUPPORTED_PROTOCOL_VERSIONS = [
  '2026-07-28', '2025-11-25', '2025-06-18', '2025-03-26',
  '2024-11-05', '2024-10-07',
];
const LATEST_PROTOCOL_VERSION = '2026-07-28';
/** 本次会话协商确定的协议版本（在 initialize 时确定） */
let negotiatedProtocolVersion = LATEST_PROTOCOL_VERSION;
let requestSequence = 0;

function debug(...args) {
  if (process.env.DEBUG) console.error('[桥接-debug]', ...args);
}

// ── Session 管理 ──────────────────────────────────────────────────────────

function loadSession() {
  try {
    if (fs.existsSync(SESSION_FILE)) {
      const raw = fs.readFileSync(SESSION_FILE, 'utf-8');
      return JSON.parse(raw).sessionId || null;
    }
  } catch {}
  return null;
}

function saveSession(sessionId) {
  try {
    fs.writeFileSync(SESSION_FILE, JSON.stringify({ sessionId, savedAt: Date.now() }), 'utf-8');
  } catch (err) {
    console.error(`[桥接] 警告: 无法写入 session 文件: ${err.message}`);
  }
}

function clearSession() {
  try {
    if (fs.existsSync(SESSION_FILE)) fs.unlinkSync(SESSION_FILE);
  } catch {}
}

// ── JSON-RPC 错误检测 ─────────────────────────────────────────────────────

function isSessionError(json) {
  if (!json || !json.error) return false;
  const msg = (typeof json.error === 'string' ? json.error : json.error.message || '').toLowerCase();
  return msg.includes('session') || msg.includes('invalid mcp');
}

function hasOwn(object, key) {
  return Object.prototype.hasOwnProperty.call(object, key);
}

function unwrapJsonRpcResponse(json) {
  if (json && typeof json === 'object' && hasOwn(json, 'error')) {
    const err = new Error(typeof json.error === 'string' ? json.error : json.error.message || '未知错误');
    if (typeof json.error === 'object') err.jsonRpcError = json.error;
    err.isSessionError = isSessionError(json);
    throw err;
  }
  return json && typeof json === 'object' && hasOwn(json, 'result') ? json.result : json;
}

// ── SSE 解析 ──────────────────────────────────────────────────────────────

function parseSSEStream(text) {
  const events = [];
  const lines = text.split('\n');
  let currentEvent = {};
  for (const line of lines) {
    if (line.startsWith('event: ')) currentEvent.event = line.slice(7).trim();
    else if (line.startsWith('data: ')) {
      const dataStr = line.slice(6).trim();
      try { currentEvent.data = JSON.parse(dataStr); } catch { currentEvent.data = dataStr; }
    } else if (line === '' && Object.keys(currentEvent).length > 0) {
      events.push(currentEvent);
      currentEvent = {};
    }
  }
  if (Object.keys(currentEvent).length > 0) events.push(currentEvent);
  return events;
}

function parseSSEEventBlock(block) {
  const event = {};
  const dataLines = [];
  for (const line of block.split(/\r?\n/)) {
    if (line.startsWith('event:')) event.event = line.slice(6).trim();
    else if (line.startsWith('data:')) dataLines.push(line.slice(5).replace(/^ /, ''));
  }
  if (dataLines.length) {
    const data = dataLines.join('\n');
    try { event.data = JSON.parse(data); } catch { event.data = data; }
  }
  return event;
}

/**
 * Consume an MCP SSE response incrementally. Notifications are delivered
 * before the final JSON-RPC response so a stdio client can observe progress
 * while a long-running tool is still executing.
 */
async function consumeSSEStream(response, requestId, onNotification) {
  if (!response.body) return null;
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  let finalMessage = null;

  const handleBlock = async (block) => {
    const event = parseSSEEventBlock(block);
    const message = event.data;
    if (!message || typeof message !== 'object') return;
    if (message.id === requestId && (hasOwn(message, 'result') || hasOwn(message, 'error'))) {
      finalMessage = message;
      return;
    }
    if (message.method && message.id === undefined) {
      await onNotification?.(message);
    }
  };

  while (true) {
    const { value, done } = await reader.read();
    if (value) buffer += decoder.decode(value, { stream: !done });
    let separator;
    while ((separator = buffer.indexOf('\n\n')) !== -1) {
      const block = buffer.slice(0, separator);
      buffer = buffer.slice(separator + 2);
      await handleBlock(block);
    }
    if (done) break;
  }
  if (buffer.trim()) await handleBlock(buffer);
  return finalMessage;
}

// ── HTTP 请求 ─────────────────────────────────────────────────────────────

function timeoutFor(method, params = {}) {
  if (method !== 'tools/call') return DEFAULT_TOOL_TIMEOUT_MS;
  const args = params.arguments || {};
  const ceiling = LONG_TOOL.test(params.name || '')
    ? LONG_TOOL_TIMEOUT_MS
    : DEFAULT_TOOL_TIMEOUT_MS;
  const requested = Number(args.timeoutMs ?? args.timeout);
  return Number.isFinite(requested)
    ? Math.min(Math.max(requested, MIN_TOOL_TRANSPORT_TIMEOUT_MS), ceiling)
    : ceiling;
}

function backendParams(params = {}) {
  if (!USE_STATELESS_PROTOCOL) return params;
  const meta = params && typeof params._meta === 'object' && params._meta !== null
    ? params._meta
    : {};
  return {
    ...params,
    _meta: {
      ...meta,
      'io.modelcontextprotocol/protocolVersion': MCP_PROTOCOL_VERSION,
      'io.modelcontextprotocol/clientInfo': {
        name: BACKEND_CLIENT_NAME,
        version: BACKEND_CLIENT_VERSION,
      },
      'io.modelcontextprotocol/clientCapabilities': {},
    },
  };
}

function backendMethodHeaders(method, params) {
  if (!USE_STATELESS_PROTOCOL) return {};
  const headers = {
    'MCP-Protocol-Version': MCP_PROTOCOL_VERSION,
    'Mcp-Method': method,
  };
  if (method === 'tools/call' && params?.name) headers['Mcp-Name'] = params.name;
  return headers;
}

async function sendRequest(method, params = {}, options = {}) {
  const sessionId = USE_STATELESS_PROTOCOL ? null : loadSession();
  const requestParams = backendParams(params);
  const headers = {
    'Content-Type': 'application/json',
    'Accept': 'text/event-stream, application/json',
    Origin: MCP_ORIGIN,
    ...backendMethodHeaders(method, requestParams),
  };
  if (CHROME_MCP_API_KEY) headers.Authorization = `Bearer ${CHROME_MCP_API_KEY}`;
  if (!USE_STATELESS_PROTOCOL && sessionId && method !== 'initialize') {
    headers['Mcp-Session-Id'] = sessionId;
  }

  const isNotification = method === 'close' || method.startsWith('notifications/');
  const body = isNotification
    ? { jsonrpc: '2.0', method, params: requestParams }
    : { jsonrpc: '2.0', id: `${Date.now()}-${++requestSequence}`, method, params: requestParams };

  const requestTimeoutMs = timeoutFor(method, params);
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), requestTimeoutMs);
  let response;
  try {
    response = await fetch(MCP_URL, {
      method: 'POST', headers,
      body: JSON.stringify(body),
      signal: controller.signal,
    });
  } catch (err) {
    clearTimeout(timeout);
    if (err.name === 'AbortError') throw new Error(`请求超时 (${requestTimeoutMs}ms): ${method}`);
    if (err.code === 'ECONNREFUSED') throw new Error(`无法连接 MCP 服务: ${MCP_URL} — 请确认服务已启动`);
    throw new Error(`网络错误: ${err.message}`);
  }
  try {
    if (!response.ok) {
      const detail = (await response.text()).trim().replace(/\s+/g, ' ').slice(0, 300);
      throw new Error(`MCP HTTP ${response.status}${detail ? `: ${detail}` : ''}`);
    }
    const newSessionId = response.headers.get('Mcp-Session-Id');
    if (!USE_STATELESS_PROTOCOL && newSessionId) saveSession(newSessionId);

    const contentType = (response.headers.get('Content-Type') || '').toLowerCase();

    if (contentType.includes('text/event-stream')) {
      const responseMessage = await consumeSSEStream(response, body.id, options.onNotification);
      if (responseMessage) return unwrapJsonRpcResponse(responseMessage);
      throw new Error(`MCP SSE 响应缺少请求 ${body.id} 的 JSON-RPC 结果`);
    } else {
      const json = await response.json();
      if (!json || typeof json !== 'object' || (!hasOwn(json, 'result') && !hasOwn(json, 'error'))) {
        throw new Error('MCP HTTP 响应不是有效的 JSON-RPC 结果');
      }
      return unwrapJsonRpcResponse(json);
    }
  } catch (err) {
    if (err.name === 'AbortError') throw new Error(`请求超时 (${requestTimeoutMs}ms): ${method}`);
    if (err.code === 'ECONNREFUSED') throw new Error(`无法连接 MCP 服务: ${MCP_URL} — 请确认服务已启动`);
    if (err.message && err.message.startsWith('MCP ')) throw err;
    throw new Error(`网络错误: ${err.message}`);
  } finally {
    clearTimeout(timeout);
  }
}

// ── 带重试的调用 ─────────────────────────────────────────────────────────

async function callWithRetry(method, params = {}, options = {}) {
  let lastError;
  for (let attempt = 1; attempt <= MAX_RETRIES + 1; attempt++) {
    try {
      return await sendRequest(method, params, options);
    } catch (err) {
      lastError = err;
      if (!USE_STATELESS_PROTOCOL && (err.isSessionError || (err.message && (
        err.message.toLowerCase().includes('session') ||
        err.message.toLowerCase().includes('invalid mcp')
      )))) {
        clearSession();
        if (attempt <= MAX_RETRIES) {
          console.error(`[桥接] Session 已过期，清理后重试 (${attempt}/${MAX_RETRIES})...`);
          continue;
        }
        console.error('[桥接] Session 重试耗尽，尝试重新初始化...');
        try {
          await sendRequest('initialize', legacyInitializeParams());
          console.error('[桥接] 重新初始化成功，重试原请求...');
          return await sendRequest(method, params, options);
        } catch (initErr) {
          throw new Error(`Session 恢复失败: ${initErr.message}`);
        }
      }
      if (attempt <= MAX_RETRIES) {
        console.error(`[桥接] 请求失败，正在重试 (${attempt}/${MAX_RETRIES}): ${err.message}`);
        continue;
      }
      throw err;
    }
  }
  throw lastError;
}

function legacyInitializeParams() {
  return {
    protocolVersion: MCP_PROTOCOL_VERSION,
    capabilities: { roots: { listChanged: false }, sampling: {} },
    clientInfo: { name: BACKEND_CLIENT_NAME, version: BACKEND_CLIENT_VERSION },
  };
}

async function initializeBackend() {
  return USE_STATELESS_PROTOCOL
    ? callWithRetry('server/discover')
    : callWithRetry('initialize', legacyInitializeParams());
}

// ── 从 stdin 读取 JSON（CLI --stdin 模式）────────────────────────────────

function readStdin() {
  return new Promise((resolve, reject) => {
    if (process.stdin.isTTY) {
      return reject(new Error('stdin 模式需要管道输入，例如: echo \'{"key":"value"}\' | node mcp-bridge.js call ... --stdin'));
    }
    const chunks = [];
    let timeout;
    process.stdin.setEncoding('utf-8');
    process.stdin.on('data', chunk => chunks.push(chunk));
    process.stdin.on('end', () => {
      clearTimeout(timeout);
      const input = chunks.join('').trim();
      if (!input) return reject(new Error('stdin 为空'));
      try { resolve(JSON.parse(input)); }
      catch (e) { reject(new Error(`stdin 内容不是有效 JSON: ${e.message}`)); }
    });
    process.stdin.on('error', err => { clearTimeout(timeout); reject(err); });
    timeout = setTimeout(() => {
      if (!process.stdin.readableEnded) { process.stdin.destroy(); reject(new Error(`stdin 读取超时 (${STDIN_TIMEOUT_MS}ms)`)); }
    }, STDIN_TIMEOUT_MS);
  });
}

// ── MCP Server 模式（--server）────────────────────────────────────────────

/**
 * stdin 持久输入缓冲：常驻监听 data 事件，把收到的字节累积到缓冲区。
 * 这样多条 MCP 消息即使合并进同一个 chunk 也不会丢失（原一次性 listener 实现会丢）。
 */
let mcpInputBuffer = Buffer.alloc(0);
let mcpInputWaiters = [];
let mcpInputEnded = false;
/** 客户端使用的 stdio framing：'content-length'（标准 MCP/LSP）或 'newline'（裸 JSON 行，Reasonix 用这种） */
let clientFraming = 'content-length';
const DIAG_IN = path.join(os.tmpdir(), 'bridge-diag-in.log');
const DIAG_OUT = path.join(os.tmpdir(), 'bridge-diag-out.log');

process.stdin.on('data', chunk => {
  try { fs.appendFileSync(DIAG_IN, chunk); } catch {}
  mcpInputBuffer = Buffer.concat([mcpInputBuffer, Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk)]);
  const waiter = mcpInputWaiters.shift();
  if (waiter) waiter();
});
process.stdin.on('end', () => {
  mcpInputEnded = true;
  const waiter = mcpInputWaiters.shift();
  if (waiter) waiter();
});
process.stdin.on('error', () => {
  mcpInputEnded = true;
  const waiter = mcpInputWaiters.shift();
  if (waiter) waiter();
});

/**
 * 从 stdin 读取一个完整的 JSON-RPC 消息。
 *
 * 同时支持两种 stdio framing（增量解析，兼容单 chunk 多消息）：
 *   1. Content-Length 帧（标准 MCP）：
 *        Content-Length: N\r\n\r\n{body 恰好 N 字节}
 *   2. newline-delimited JSON（Reasonix 等客户端）：
 *        {jsonrpc...}\n
 *
 * 识别到哪种格式就记录到 clientFraming，响应用相同格式回写。
 */
async function readMcpMessage() {
  while (true) {
    if (mcpInputBuffer.length === 0) {
      if (mcpInputEnded) throw new Error('stdin closed');
      await new Promise(resolve => mcpInputWaiters.push(resolve));
      continue;
    }
    const firstByte = mcpInputBuffer[0];
    if (firstByte === 0x7B /* '{'：newline-delimited JSON（Reasonix 风格） */) {
      const nlIdx = mcpInputBuffer.indexOf('\n');
      if (nlIdx !== -1) {
        const line = mcpInputBuffer.subarray(0, nlIdx).toString('utf-8').trim();
        mcpInputBuffer = mcpInputBuffer.subarray(nlIdx + 1);
        if (line) {
          try {
            clientFraming = 'newline';
            return JSON.parse(line);
          } catch (e) {
            // 非法 JSON 行：跳过，继续解析
          }
        }
        continue;
      }
      // 无换行：数据不完整或已结束，走末尾统一处理
    } else {
      // Content-Length 帧（标准 MCP/LSP）
      const headerEnd = mcpInputBuffer.indexOf('\r\n\r\n');
      if (headerEnd !== -1) {
        const header = mcpInputBuffer.subarray(0, headerEnd).toString('utf-8');
        const match = header.match(/Content-Length:\s*(\d+)/i);
        if (!match) {
          // 丢弃无效头部，避免阻塞后续消息
          mcpInputBuffer = mcpInputBuffer.subarray(headerEnd + 4);
          throw new Error('MCP 消息缺少 Content-Length 头');
        }
        const contentLength = parseInt(match[1], 10);
        const bodyStart = headerEnd + 4;
        if (mcpInputBuffer.length >= bodyStart + contentLength) {
          const body = mcpInputBuffer.subarray(bodyStart, bodyStart + contentLength).toString('utf-8');
          mcpInputBuffer = mcpInputBuffer.subarray(bodyStart + contentLength);
          clientFraming = 'content-length';
          try {
            return JSON.parse(body);
          } catch (e) {
            throw new Error(`JSON 解析失败: ${e.message}`);
          }
        }
      }
    }
    // 已缓冲数据不足一整条消息：等待更多输入
    if (mcpInputEnded) {
      // stdin 已关闭但 buffer 还有残留：尝试把残留当作最后一条 newline JSON
      const rest = mcpInputBuffer.toString('utf-8').trim();
      if (rest) {
        mcpInputBuffer = Buffer.alloc(0);
        try {
          clientFraming = 'newline';
          return JSON.parse(rest);
        } catch { /* 忽略残留 */ }
      }
      throw new Error('stdin closed');
    }
    await new Promise(resolve => mcpInputWaiters.push(resolve));
  }
}

/** 向 stdout 写入一个 JSON-RPC 消息（自动匹配客户端的 framing） */
function writeMcpMessage(msg) {
  const body = JSON.stringify(msg);
  let payload;
  if (clientFraming === 'newline') {
    payload = body + '\n';
  } else {
    const byteLen = Buffer.byteLength(body, 'utf-8');
    payload = `Content-Length: ${byteLen}\r\n\r\n${body}`;
  }
  try { fs.appendFileSync(DIAG_OUT, payload); } catch {}
  process.stdout.write(payload);
}

/** 统一的后端工具响应提取 */
function extractTools(backendResult) {
  if (!backendResult) return [];
  if (Array.isArray(backendResult.tools)) return backendResult.tools;
  if (backendResult.result && Array.isArray(backendResult.result.tools)) return backendResult.result.tools;
  if (Array.isArray(backendResult)) return backendResult;
  return [];
}

let toolsCache = null;
let toolsCacheExpiresAt = 0;

async function listTools() {
  if (USE_STATELESS_PROTOCOL && toolsCache && toolsCacheExpiresAt > Date.now()) {
    return toolsCache;
  }

  const result = await callWithRetry('tools/list');
  const payload = result && typeof result === 'object' && !Array.isArray(result)
    ? { ...result, tools: extractTools(result) }
    : { tools: extractTools(result) };
  const ttlMs = Number(payload.ttlMs);
  if (USE_STATELESS_PROTOCOL && Number.isFinite(ttlMs) && ttlMs > 0) {
    toolsCache = payload;
    toolsCacheExpiresAt = Date.now() + ttlMs;
  }
  return payload;
}

function writeBackendResult(id, result) {
  writeMcpMessage({ jsonrpc: '2.0', id, result });
}

function writeBackendError(id, err) {
  writeMcpMessage({
    jsonrpc: '2.0',
    id,
    error: err.jsonRpcError || { code: -32603, message: err.message },
  });
}

async function forwardRequest(id, method, params, options = {}) {
  try {
    const result = await callWithRetry(method, params, options);
    writeBackendResult(id, result);
  } catch (err) {
    writeBackendError(id, err);
  }
}

/** 处理单个 MCP 请求 */
async function handleRequest(id, method, params) {
  debug('收到请求:', method, JSON.stringify(params).substring(0, 200));

  switch (method) {
    case 'initialize': {
      // 版本协商：接受客户端请求的版本（如果在支持列表中）
      const requestedVersion = params && params.protocolVersion;
      const negotiated = SUPPORTED_PROTOCOL_VERSIONS.includes(requestedVersion)
        ? requestedVersion
        : LATEST_PROTOCOL_VERSION;
      negotiatedProtocolVersion = negotiated;

      writeMcpMessage({
        jsonrpc: '2.0',
        id,
        result: {
          protocolVersion: negotiated,
          capabilities: {
            tools: {},
            ...(USE_STATELESS_PROTOCOL ? {} : {
              roots: { listChanged: false },
              sampling: {},
            }),
          },
          serverInfo: {
            name: SERVER_NAME,
            version: SERVER_VERSION,
          },
        },
      });
      return;
    }

    case 'tools/list': {
      try {
        writeBackendResult(id, await listTools());
      } catch (err) {
        console.error(`[mcp-server] tools/list 代理失败: ${err.message}`);
        writeMcpMessage({
          jsonrpc: '2.0',
          id,
          error: { code: -32603, message: `Backend tools/list failed: ${err.message}` },
        });
      }
      return;
    }

    case 'tools/call': {
      const { name, arguments: args } = params;
      if (!name) {
        writeMcpMessage({
          jsonrpc: '2.0',
          id,
          error: { code: -32602, message: 'Missing tool name' },
        });
        return;
      }
      try {
        const callParams = USE_STATELESS_PROTOCOL
          ? { ...params, name, arguments: args || {} }
          : {
              name,
              arguments: args || {},
              ...(params && params._meta ? { _meta: params._meta } : {}),
            };
        const result = await callWithRetry('tools/call', callParams, {
          onNotification: (notification) => writeMcpMessage(notification),
        });

        // 新协议的 input_required/task/resultType 字段必须完整透传。
        const response = result && typeof result === 'object' && !Array.isArray(result)
          && (Array.isArray(result.content) || result.resultType || result.task)
          ? result
          : { content: [{
              type: 'text',
              text: typeof result === 'string' ? result : JSON.stringify(result, null, 2),
            }] };
        writeBackendResult(id, response);
      } catch (err) {
        writeBackendError(id, err);
      }
      return;
    }

    case 'ping':
      if (USE_STATELESS_PROTOCOL) {
        await forwardRequest(id, method, params);
        return;
      }
      writeMcpMessage({
        jsonrpc: '2.0',
        id,
        result: {},
      });
      return;

    case 'server/discover':
    case 'resources/list':
    case 'resources/read':
    case 'prompts/list':
    case 'prompts/get':
    case 'tasks/get':
    case 'tasks/update':
    case 'tasks/result':
      if (USE_STATELESS_PROTOCOL) {
        await forwardRequest(id, method, params);
        return;
      }
      if (method === 'resources/list') {
        writeMcpMessage({ jsonrpc: '2.0', id, result: { resources: [] } });
      } else if (method === 'prompts/list') {
        writeMcpMessage({ jsonrpc: '2.0', id, result: { prompts: [] } });
      } else {
        writeMcpMessage({
          jsonrpc: '2.0',
          id,
          error: { code: -32601, message: `Method not found: ${method}` },
        });
      }
      return;

    default:
      // Forward extensions and future MCP methods without another bridge release.
      await forwardRequest(id, method, params);
  }
}

async function startMcpServer() {
  // 先开始监听 stdin，确保 Reasonix 的 initialize 请求不会丢失
  console.error('[mcp-server] 正在监听 stdin（MCP stdio 协议）...');

  // 在后台异步初始化后端连接，不阻塞 stdin 处理
  let backendReady = false;
  let backendTools = [];

  async function initBackendAsync() {
    try {
      console.error('[mcp-server] 正在连接后端 MCP 服务...');
      const initResult = await initializeBackend();
      console.error('[mcp-server] 后端 MCP 连接成功');

      try {
        const toolsResult = await listTools();
        backendTools = extractTools(toolsResult);
        console.error(`[mcp-server] 已加载 ${backendTools.length} 个工具`);
      } catch (err) {
        console.error(`[mcp-server] 获取工具列表失败: ${err.message}`);
      }
      backendReady = true;
    } catch (err) {
      console.error(`[mcp-server] 后端连接失败: ${err.message}`);
      console.error('[mcp-server] 仍将继续监听，但后端工具可能不可用');
    }
  }

  // 异步启动后端初始化
  initBackendAsync().catch(err => {
    console.error(`[mcp-server] 后端初始化异常: ${err.message}`);
  });

  // 主循环：立即开始处理 stdin 请求
  while (true) {
    let message;
    try {
      message = await readMcpMessage();
    } catch (err) {
      if (err.message === 'stdin closed') {
        console.error('[mcp-server] stdin 关闭，优雅退出');
        if (!USE_STATELESS_PROTOCOL) {
          try { await sendRequest('close'); } catch {}
        }
        if (!USE_STATELESS_PROTOCOL) clearSession();
        process.exit(0);
      }
      console.error(`[mcp-server] 读取消息失败: ${err.message}`);
      continue;
    }

    // 通知类消息（无 id）不响应
    if (!message || message.id === undefined || message.id === null) {
      continue;
    }

    // 异步处理，不阻塞后续消息
    handleRequest(message.id, message.method, message.params || {}).catch(err => {
      console.error(`[mcp-server] 请求处理异常: ${err.message}`);
      writeMcpMessage({
        jsonrpc: '2.0',
        id: message.id,
        error: { code: -32603, message: `Internal error: ${err.message}` },
      });
    });
  }
}

// ── 工具函数 ───────────────────────────────────────────────────────────────

function isPowerShell() {
  const env = process.env;
  return !!(
    (env.PSModulePath) ||
    (env.WT_SESSION) ||
    (env.SHELL && env.SHELL.includes('powershell')) ||
    (process.platform === 'win32' && !env.SHELL)
  );
}

function getScriptPath() {
  return __filename;
}

// ── CLI 入口 ──────────────────────────────────────────────────────────────

async function main() {
  const args = process.argv.slice(2);
  const command = args[0];

  // 无参数：显示帮助
  if (!command) {
    const psHint = isPowerShell()
      ? '\n🔵 检测到 PowerShell 环境！建议使用 --stdin 模式。\n   详见: node mcp-bridge.js call <method> --stdin'
      : '';
    console.log(`
mcp-bridge.js — Streamable HTTP MCP 桥接工具 (v${SERVER_VERSION})

支持协议版本: ${SUPPORTED_PROTOCOL_VERSIONS.join(', ')}

模式:
  --server                          以 stdio MCP Server 模式运行
  path                              显示脚本绝对路径
  init                              初始化连接
  call <method> [params|--stdin]    调用方法
  ping                              心跳保活
  close                             关闭连接

环境变量:
  MCP_SERVER_URL                    后端 MCP 服务地址（默认 ${MCP_URL}）
  MCP_SERVER_ORIGIN                 后端 Origin（默认 ${MCP_ORIGIN}）
  MCP_PROTOCOL_MODE                 auto、stateless 或 legacy（默认 auto）
  MCP_PROTOCOL_VERSION              请求协议版本（默认 ${MCP_PROTOCOL_VERSION}）
  CHROME_MCP_API_KEY                可选后端 API Key（Bearer 转发）
  DEBUG                             设为 1 开启调试日志

--stdin 示例（推荐，避免 shell 转义）:
  echo '{"name":"x","arguments":{}}' | node mcp-bridge.js call tools/call --stdin

PowerShell heredoc 示例:
  $body = @'
  {"name":"chrome_navigate","arguments":{"url":"https://example.com?lang=en"}}
  '@
  $body | node mcp-bridge.js call tools/call --stdin

MCP Server 配置示例（.mcp.json / claude_desktop_config.json）:
  {
    "mcpServers": {
      "chrome-bridge": {
        "command": "node",
        "args": ["${getScriptPath().replace(/\\/g, '\\\\')}", "--server"]
      }
    }
  }
${psHint}`);
    process.exit(0);
  }

  // ── --server 模式 ──────────────────────────────────────────────────
  if (command === '--server') {
    await startMcpServer();
    return;
  }

  // ── 常规 CLI 模式 ──────────────────────────────────────────────────
  try {
    let result;
    switch (command) {
      case 'init':
        result = await initializeBackend();
        if (result && result.protocolVersion) {
          negotiatedProtocolVersion = result.protocolVersion;
        }
        debug(`协商协议版本: ${negotiatedProtocolVersion}`);
        break;

      case 'path':
        console.log(getScriptPath());
        process.exit(0);
        break;

      case 'call': {
        const method = args[1];
        if (!method) { console.error('错误: 请指定方法名'); process.exit(1); }
        let params = {};
        if (args[2]) {
          if (args[2] === '--stdin') {
            params = await readStdin();
          } else {
            try { params = JSON.parse(args[2]); }
            catch {
              console.error('错误: params 不是有效 JSON');
              console.error('收到:', args[2].substring(0, 200));
              if (isPowerShell()) {
                console.error('\n💡 检测到 PowerShell 环境！请改用 heredoc + --stdin 模式：');
                console.error('');
                console.error('   $body = @\'');
                console.error(`   ${args[2].replace(/&/g, '`&').substring(0, 200)}`);
                console.error("   '@");
                console.error(`   $body | node mcp-bridge.js call ${method.replace(/'/g, "''")} --stdin`);
                console.error('');
              } else {
                console.error('\n💡 提示: 参数含 & 等特殊字符时，请用 --stdin 模式:');
                console.error(`  echo '${args[2].substring(0, 100)}' | node mcp-bridge.js call ${method} --stdin`);
              }
              process.exit(1);
            }
          }
        }
        result = await callWithRetry(method, params, {
          onNotification: (notification) => {
            console.error(`[MCP notification] ${JSON.stringify(notification)}`);
          },
        });
        break;
      }

      case 'ping':
        result = await callWithRetry('ping');
        break;

      case 'close':
        if (!USE_STATELESS_PROTOCOL) {
          try { await sendRequest('close'); } catch {}
          clearSession();
        }
        console.log(USE_STATELESS_PROTOCOL ? '无状态连接已关闭' : '连接已关闭，session 已清理');
        process.exit(0);
        break;

      default:
        console.error(`错误: 未知命令 "${command}"`);
        process.exit(1);
    }
    console.log(JSON.stringify(result, null, 2));
  } catch (err) {
    console.error(`[桥接] 错误: ${err.message}`);
    if (err.jsonRpcError) console.error(`[桥接] JSON-RPC 错误码: ${err.jsonRpcError.code}`);
    process.exit(1);
  }
}

main();
