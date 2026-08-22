// Reasonix Browser Relay — Background Service Worker
//
// Bridges CDP commands from Reasonix's relay server (WebSocket) to the
// Chrome Debugger API (chrome.debugger) on the currently active tab.

let ws = null;
let wsReconnectTimer = null;
let authTimer = null;
let authSucceeded = false;
let connectedAddr = '';
let authToken = '';
// Multi-tab attach: every explicitly attached tab keeps its own debugger
// connection; CDP commands target the currently selected tab (activeTabId).
let attachedTabs = new Map(); // tabId -> { url, title, lastUsed }
let activeTabId = null;
// Reconnect bookkeeping: set when the user intentionally disconnects so the
// WS onclose handler does not schedule a reconnect.
let intentionalDisconnect = false;
let reconnectAttempts = 0;
// Set when the server rejected our token (auth_error or a 4000 close such as
// "token rotated"). Retrying with the same stored token can only fail again,
// so auto-reconnect stays off until the user connects with a fresh token.
let authRejected = false;
// Idle cleanup: tabs with no CDP activity for this long are detached (guide §9.3).
const ATTACH_IDLE_TIMEOUT_MS = 30 * 60 * 1000; // 30 minutes
// Console messages and network requests cache per tab.
const MAX_CACHED_CONSOLE_MSGS = 200;
const MAX_CACHED_NETWORK_REQS = 200;

// Chrome debugger events: cache console messages and network requests.
chrome.debugger.onEvent.addListener((source, method, params) => {
  const tabId = source.tabId;
  if (!attachedTabs.has(tabId)) return;
  const info = attachedTabs.get(tabId);

  if (method === 'Console.messageAdded' && params && params.message) {
    info.consoleMessages.push(params.message);
    if (info.consoleMessages.length > MAX_CACHED_CONSOLE_MSGS) {
      info.consoleMessages.shift();
    }
  } else if (method === 'Network.requestWillBeSent' && params) {
    info.networkRequests.push({
      id: params.requestId,
      url: params.request.url,
      method: params.request.method || 'GET',
      type: params.type || '',
      timestamp: params.timestamp || 0,
      headers: params.request.headers || {}
    });
    if (info.networkRequests.length > MAX_CACHED_NETWORK_REQS) {
      info.networkRequests.shift();
    }
  } else if (method === 'Network.responseReceived' && params) {
    const req = info.networkRequests.find(r => r.id === params.requestId);
    if (req) {
      req.statusCode = params.response.status;
      req.statusText = params.response.statusText;
      req.mimeType = params.response.mimeType;
    }
  }
});

// ── Connection management ──────────────────────────────────────────────────

function connect(addr, token) {
  closeSocket();

  intentionalDisconnect = false;
  reconnectAttempts = 0;
  authRejected = false;
  connectedAddr = addr;
  authToken = token;

  try {
    ws = new WebSocket(addr);
  } catch (e) {
    broadcastState({ status: 'error', error: 'Invalid WebSocket address: ' + e.message });
    return;
  }

  ws.onopen = () => {
    broadcastState({ status: 'connected' });
    ws.send(JSON.stringify({ type: 'auth', token: token, info: 'reasonix-relay-extension-v1' }));
    // If the server never replies with auth_ok/auth_error (e.g. wrong port that
    // happens to accept WebSockets), surface an error instead of hanging.
    clearTimeout(authTimer);
    authTimer = setTimeout(() => {
      if (ws && ws.readyState === WebSocket.OPEN && !authSucceeded) {
        broadcastState({ status: 'error', error: 'Authentication timeout: server did not respond. Check the port and token.' });
        ws.close();
      }
    }, 5000);
  };

  ws.onmessage = (event) => {
    try {
      const msg = JSON.parse(event.data);
      handleMessage(msg);
    } catch (e) {
      console.error('[browser-relay] failed to parse message:', e);
    }
  };

  ws.onclose = (event) => {
    clearTimeout(authTimer);
    authTimer = null;
    authSucceeded = false;
    broadcastState({ status: 'disconnected', error: event.reason || 'connection closed' });
    ws = null;
    detachDebugger();
    connectedAddr = '';
    authToken = '';
    // A server-initiated 4000 close ("token rotated" / invalid token) means the
    // stored token is stale; retrying it would only fail again, so stop
    // auto-reconnect until the user reconnects with a fresh token.
    if (event.code === 4000) {
      authRejected = true;
    }
    if (!intentionalDisconnect && !authRejected) {
      scheduleReconnect();
    }
  };

  ws.onerror = (event) => {
    broadcastState({ status: 'error', error: 'WebSocket error' });
  };
}

// scheduleReconnect retries the saved connection with exponential backoff
// (2s, 4s, 8s, ... capped at 30s). Stops when the user disconnects or a
// connect() supersedes it.
function scheduleReconnect() {
  if (wsReconnectTimer) return; // already scheduled
  const delay = Math.min(2000 * Math.pow(2, reconnectAttempts), 30000);
  reconnectAttempts++;
  wsReconnectTimer = setTimeout(() => {
    wsReconnectTimer = null;
    chrome.storage.local.get(['relayAddr', 'relayToken', 'autoConnect'], (data) => {
      if (intentionalDisconnect || !data.autoConnect || !data.relayAddr || !data.relayToken) {
        return;
      }
      connect(data.relayAddr, data.relayToken);
    });
  }, delay);
}

// Close the WebSocket and reset connection state, but keep attached tabs and
// their debugger connections — reconnecting should not wipe pages the user
// already attached.
function closeSocket() {
  if (wsReconnectTimer) {
    clearTimeout(wsReconnectTimer);
    wsReconnectTimer = null;
  }
  reconnectAttempts = 0;
  clearTimeout(authTimer);
  authTimer = null;
  authSucceeded = false;
  if (ws) {
    ws.onclose = null; // prevent handler from running
    ws.close();
    ws = null;
  }
  connectedAddr = '';
  authToken = '';
}

function disconnect() {
  intentionalDisconnect = true;
  closeSocket();
  detachDebugger();
  broadcastState({ status: 'disconnected' });
}

// ── Message handling ───────────────────────────────────────────────────────

function handleMessage(msg) {
  switch (msg.type) {
    case 'auth_ok':
      authSucceeded = true;
      clearTimeout(authTimer);
      authTimer = null;
      broadcastState({ status: 'authorized', version: msg.version });
      break;

    case 'auth_error':
      authRejected = true;
      broadcastState({ status: 'error', error: 'Authentication failed: ' + (msg.error || 'invalid token') });
      ws.close();
      break;

    case 'cdp_command':
      handleCDPCommand(msg);
      break;

    case 'tab_command':
      handleTabCommand(msg);
      break;

    default:
      console.warn('[browser-relay] unknown message type:', msg.type);
  }
}

function handleCDPCommand(msg) {
  cleanupIdleTabs();
  const tabId = activeTabId;
  if (!tabId) {
    sendCDPResult(msg.id, null, 'No tab attached. Click "Attach Current Tab" in the extension popup first.');
    return;
  }
  touchActiveTab();

  // Map "Page.navigate" style method names to chrome.debugger commands.
  const method = msg.method;
  const params = msg.params || {};

  // Some CDP methods need special handling — for most, pass through directly.
  chrome.debugger.sendCommand({ tabId: tabId }, method, params, (result) => {
    if (chrome.runtime.lastError) {
      sendCDPResult(msg.id, null, chrome.runtime.lastError.message);
    } else {
      sendCDPResult(msg.id, result, null);
    }
  });
}

function sendCDPResult(id, result, error) {
  if (!ws || ws.readyState !== WebSocket.OPEN) return;
  ws.send(JSON.stringify({
    type: 'cdp_result',
    id: id,
    result: result,
    error: error
  }));
}

// ── Tab management (non-CDP extension commands) ────────────────────────────

function handleTabCommand(msg) {
  switch (msg.command) {
    case 'list_pages':
      chrome.tabs.query({}, (tabs) => {
        const pages = tabs.map((t, i) => ({
          index: i,
          tabId: t.id,
          title: t.title || '',
          url: t.url || '',
          active: t.active,
          windowId: t.windowId
        }));
        sendTabResult(msg.id, pages, null);
      });
      break;

    case 'select_page':
      if (msg.tabId == null) {
        sendTabResult(msg.id, null, 'tabId is required');
        return;
      }
      // Select an attached tab as the CDP target. Attaching stays untouched —
      // other attached tabs keep their debugger connections.
      if (!attachedTabs.has(msg.tabId)) {
        sendTabResult(msg.id, null, 'Tab is not attached. Attach it first (popup "Attach Current Tab" or browser_attach_page).');
        return;
      }
      activeTabId = msg.tabId;
      chrome.tabs.get(msg.tabId, (tab) => {
        const info = { tabId: msg.tabId, url: tab ? (tab.url || '') : '', title: tab ? tab.title : '' };
        sendTabResult(msg.id, info, null);
      });
      break;

    case 'new_page':
      chrome.tabs.create({ url: msg.url || 'about:blank', active: true }, (tab) => {
        // Auto-attach so the new tab is immediately usable as CDP target.
        attachDebuggerToTab(tab.id, tab.url || '', tab.title || '');
        sendTabResult(msg.id, { tabId: tab.id, url: tab.url || '', title: tab.title || '' }, null);
      });
      break;

    case 'close_page':
      if (msg.tabId == null) {
        sendTabResult(msg.id, null, 'tabId is required');
        return;
      }
      detachTab(msg.tabId);
      chrome.tabs.remove(msg.tabId, () => {
        if (chrome.runtime.lastError) {
          sendTabResult(msg.id, null, chrome.runtime.lastError.message);
          return;
        }
        sendTabResult(msg.id, { closed: true, tabId: msg.tabId }, null);
      });
      break;

    case 'attach_page':
      if (msg.tabId == null) {
        sendTabResult(msg.id, null, 'tabId is required');
        return;
      }
      if (attachedTabs.has(msg.tabId)) {
        // Already attached — just make it the active target.
        activeTabId = msg.tabId;
        sendTabResult(msg.id, { tabId: msg.tabId, attached: true, active: true }, null);
        return;
      }
      chrome.tabs.get(msg.tabId, (tab) => {
        attachDebuggerToTab(msg.tabId, tab ? (tab.url || '') : '', tab ? (tab.title || '') : '');
        sendTabResult(msg.id, { tabId: msg.tabId, attached: true, active: true }, null);
      });
      break;

    case 'detach_page':
      if (msg.tabId == null) {
        sendTabResult(msg.id, null, 'tabId is required');
        return;
      }
      detachTab(msg.tabId);
      sendTabResult(msg.id, { detached: true, tabId: msg.tabId }, null);
      break;

    case 'list_attached':
      const list = Array.from(attachedTabs.entries()).map(([tabId, info]) => ({
        tabId: tabId,
        url: info.url,
        title: info.title,
        active: tabId === activeTabId
      }));
      sendTabResult(msg.id, list, null);
      break;

    case 'list_console_messages':
      if (!attachedTabs.has(activeTabId)) {
        sendTabResult(msg.id, null, 'No active tab');
        return;
      }
      const msgs = attachedTabs.get(activeTabId).consoleMessages || [];
      // Clear after reading if requested.
      if (msg.params && msg.params.clear) {
        attachedTabs.get(activeTabId).consoleMessages = [];
      }
      sendTabResult(msg.id, msgs, null);
      break;

    case 'list_network_requests':
      if (!attachedTabs.has(activeTabId)) {
        sendTabResult(msg.id, null, 'No active tab');
        return;
      }
      const reqs = attachedTabs.get(activeTabId).networkRequests || [];
      // Clear after reading if requested.
      if (msg.params && msg.params.clear) {
        attachedTabs.get(activeTabId).networkRequests = [];
      }
      sendTabResult(msg.id, reqs, null);
      break;

    default:
      sendTabResult(msg.id, null, 'Unknown tab command: ' + msg.command);
  }
}

function sendTabResult(id, result, error) {
  if (!ws || ws.readyState !== WebSocket.OPEN) return;
  ws.send(JSON.stringify({
    type: 'tab_result',
    id: id,
    result: result,
    error: error
  }));
}

// ── Debugger management ────────────────────────────────────────────────────

function attachToActiveTab() {
  chrome.tabs.query({ active: true, currentWindow: true }, (tabs) => {
    if (chrome.runtime.lastError) {
      broadcastState({ status: 'error', error: 'Failed to query tabs: ' + chrome.runtime.lastError.message });
      return;
    }
    if (!tabs || tabs.length === 0) {
      broadcastState({ status: 'error', error: 'No active tab found' });
      return;
    }

    const tabId = tabs[0].id;
    const tabUrl = tabs[0].url || '';
    const tabTitle = tabs[0].title || '';
    attachDebuggerToTab(tabId, tabUrl, tabTitle);
  });
}

function attachDebuggerToTab(tabId, tabUrl, tabTitle) {
  if (attachedTabs.has(tabId)) {
    // Already attached — just make it the active CDP target.
    activeTabId = tabId;
    broadcastState(getState());
    return;
  }

  chrome.debugger.attach({ tabId: tabId }, '1.3', () => {
    if (chrome.runtime.lastError) {
      broadcastState({
        status: 'error',
        error: 'Failed to attach debugger: ' + chrome.runtime.lastError.message
      });
      return;
    }

    attachedTabs.set(tabId, { url: tabUrl || '', title: tabTitle || '', lastUsed: Date.now(), consoleMessages: [], networkRequests: [] });
    activeTabId = tabId;
    broadcastState(getState());

    // Enable necessary domains.
    chrome.debugger.sendCommand({ tabId: tabId }, 'Page.enable', {}, () => {
      chrome.runtime.lastError; // ignore
    });
    chrome.debugger.sendCommand({ tabId: tabId }, 'Runtime.enable', {}, () => {
      chrome.runtime.lastError; // ignore
    });
    chrome.debugger.sendCommand({ tabId: tabId }, 'DOM.enable', {}, () => {
      chrome.runtime.lastError; // ignore
    });
    chrome.debugger.sendCommand({ tabId: tabId }, 'Console.enable', {}, () => {
      chrome.runtime.lastError; // ignore
    });
    chrome.debugger.sendCommand({ tabId: tabId }, 'Network.enable', {}, () => {
      chrome.runtime.lastError; // ignore
    });
  });
}

function detachTab(tabId) {
  if (!attachedTabs.has(tabId)) return;
  chrome.debugger.detach({ tabId: tabId }, () => {
    chrome.runtime.lastError; // ignore
  });
  attachedTabs.delete(tabId);
  if (activeTabId === tabId) {
    // Fall back to the first remaining attached tab, if any.
    activeTabId = attachedTabs.size > 0 ? Array.from(attachedTabs.keys())[0] : null;
  }
  broadcastState(getState());
}

// Detach tabs that have seen no CDP activity for ATTACH_IDLE_TIMEOUT_MS
// (guide §9.3). Lazy check on message activity — a background service worker
// cannot rely on a long-lived timer.
function cleanupIdleTabs() {
  const now = Date.now();
  const stale = [];
  attachedTabs.forEach((info, tabId) => {
    if (now - (info.lastUsed || 0) > ATTACH_IDLE_TIMEOUT_MS) stale.push(tabId);
  });
  stale.forEach((tabId) => detachTab(tabId));
  return stale.length;
}

// Mark the active tab as recently used when CDP traffic flows to it.
function touchActiveTab() {
  if (activeTabId !== null && attachedTabs.has(activeTabId)) {
    attachedTabs.get(activeTabId).lastUsed = Date.now();
  }
}

function detachDebugger() {
  for (const tabId of Array.from(attachedTabs.keys())) {
    chrome.debugger.detach({ tabId: tabId }, () => {
      chrome.runtime.lastError; // ignore
    });
  }
  attachedTabs.clear();
  activeTabId = null;
}

// Listen for debugger detach events (e.g. user closes DevTools).
chrome.debugger.onDetach.addListener((source, reason) => {
  const tabId = source && source.tabId;
  if (tabId != null && attachedTabs.has(tabId)) {
    attachedTabs.delete(tabId);
    if (activeTabId === tabId) {
      activeTabId = attachedTabs.size > 0 ? Array.from(attachedTabs.keys())[0] : null;
    }
    // Do NOT re-attach automatically — attach is explicit (privacy by default).
    broadcastState(getState());
    broadcastState({ status: 'authorized', warning: 'Tab #' + tabId + ' detached: ' + reason + '. Attach it again if still needed.' });
  }
});

// Keep attached tab titles fresh: SPA navigation (e.g. switching between docs
// on a single-page app) changes titles after attach without re-attaching.
chrome.tabs.onUpdated.addListener((tabId, changeInfo, tab) => {
  if (changeInfo.title && attachedTabs.has(tabId)) {
    attachedTabs.get(tabId).title = changeInfo.title;
    broadcastState(getState());
  }
});

// ── Popup communication ────────────────────────────────────────────────────

// Port from popup for state updates.
let popupPort = null;

chrome.runtime.onConnect.addListener((port) => {
  if (port.name === 'popup') {
    popupPort = port;

    port.onMessage.addListener((msg) => {
      switch (msg.action) {
        case 'connect':
          connect(msg.addr, msg.token);
          break;
        case 'disconnect':
          disconnect();
          break;
        case 'reconnect':
          // Reconnect using the saved configuration (exponential-backoff
          // auto-reconnect is paused while disconnected intentionally).
          chrome.storage.local.get(['relayAddr', 'relayToken'], (data) => {
            if (data.relayAddr && data.relayToken) {
              connect(data.relayAddr, data.relayToken);
            } else {
              port.postMessage(getState());
            }
          });
          break;
        case 'attach':
          attachToActiveTab();
          break;
        case 'getState':
          port.postMessage(getState());
          break;
        case 'listPages':
          chrome.tabs.query({}, (tabs) => {
            const pages = tabs.map((t) => ({
              tabId: t.id,
              title: t.title || '',
              url: t.url || '',
              active: t.active,
              windowId: t.windowId
            }));
            port.postMessage({ type: 'pages', pages: pages });
          });
          break;
        case 'selectPage':
          if (attachedTabs.has(msg.tabId)) {
            activeTabId = msg.tabId;
            port.postMessage(getState());
          } else {
            port.postMessage({ type: 'error', error: 'Tab is not attached' });
          }
          break;
        case 'detachPage':
          detachTab(msg.tabId);
          port.postMessage(getState());
          break;
      }
    });

    port.onDisconnect.addListener(() => {
      popupPort = null;
    });

    // Send current state immediately.
    port.postMessage(getState());
  }
});

// ── Options page communication ─────────────────────────────────────────────

chrome.runtime.onMessage.addListener((msg, sender, sendResponse) => {
  if (msg.action === 'connect') {
    connect(msg.addr, msg.token);
    sendResponse({ ok: true });
  }
});

function broadcastState(state) {
  const full = Object.assign({}, getState(), state);
  updateActionBadge(full);
  if (popupPort) {
    try {
      popupPort.postMessage(full);
    } catch (e) {
      popupPort = null;
    }
  }
}

// Toolbar icon: the plain Reasonix icon normally; the "-on" variant (with a
// green dot at the top-right) while connected/authorized. No badge background
// needed — the state lives inside the icon itself.
function updateActionBadge(state) {
  if (state.status === 'connected' || state.status === 'authorized') {
    chrome.action.setIcon({ path: { '16': 'icons/icon-16-on.png', '48': 'icons/icon-48-on.png', '128': 'icons/icon-128-on.png' } });
  } else {
    chrome.action.setIcon({ path: { '16': 'icons/icon-16.png', '48': 'icons/icon-48.png', '128': 'icons/icon-128.png' } });
  }
  chrome.action.setBadgeText({ text: '' });
}

function getState() {
  cleanupIdleTabs();
  const state = { status: 'disconnected' };
  if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
    state.status = ws.readyState === WebSocket.CONNECTING ? 'connecting' : 'connected';
  }
  if (authSucceeded) {
    state.status = 'authorized';
  }
  if (activeTabId !== null) {
    state.status = 'authorized';
    state.tabId = activeTabId;
    const info = attachedTabs.get(activeTabId);
    state.tabUrl = info ? info.url : '';
  }
  state.attachedTabs = Array.from(attachedTabs.entries()).map(([tabId, info]) => ({
    tabId: tabId,
    url: info.url,
    title: info.title,
    active: tabId === activeTabId
  }));
  if (connectedAddr) {
    state.addr = connectedAddr;
  }
  return state;
}

// Restore saved connection on startup (if user had enabled auto-reconnect).
chrome.storage.local.get(['relayAddr', 'relayToken', 'autoConnect'], (data) => {
  if (data.autoConnect && data.relayAddr && data.relayToken) {
    connect(data.relayAddr, data.relayToken);
  }
});

// Keep service worker alive by handling storage changes.
chrome.storage.onChanged.addListener((changes, area) => {
  if (area === 'local' && changes.autoConnect) {
    chrome.storage.local.get(['relayAddr', 'relayToken', 'autoConnect'], (data) => {
      if (data.autoConnect && data.relayAddr && data.relayToken) {
        connect(data.relayAddr, data.relayToken);
      } else if (!data.autoConnect) {
        disconnect();
      }
    });
  }
});

console.log('[browser-relay] background service worker loaded');