// Reasonix Browser Relay — Popup Script

const port = chrome.runtime.connect({ name: 'popup' });

// DOM elements
const statusBadge = document.getElementById('statusBadge');
const statusDetail = document.getElementById('statusDetail');
const attachBtn = document.getElementById('attachBtn');
const settingsBtn = document.getElementById('settingsBtn');
const reconnectBtn = document.getElementById('reconnectBtn');
const attachedSection = document.getElementById('attachedSection');
const attachedList = document.getElementById('attachedList');

// ── State management ───────────────────────────────────────────────────────

let currentState = { status: 'disconnected' };

function renderAttachedTabs(tabs) {
  const list = tabs || [];
  attachedList.innerHTML = '';
  if (list.length === 0) {
    const empty = document.createElement('div');
    empty.className = 'attached-empty';
    empty.textContent = 'No tabs attached';
    attachedList.appendChild(empty);
    return;
  }
  list.forEach((tab) => {
    const row = document.createElement('div');
    row.className = 'attached-item';

    const info = document.createElement('div');
    info.className = 'attached-item__info';
    const titleRow = document.createElement('div');
    titleRow.className = 'attached-item__title-row';
    const title = document.createElement('span');
    title.className = 'attached-item__title';
    title.textContent = tab.title || 'Tab #' + tab.tabId;
    // Tab ID sits to the right of the title on the same row.
    const idSpan = document.createElement('span');
    idSpan.className = 'attached-item__id';
    idSpan.textContent = '#' + tab.tabId;
    titleRow.appendChild(title);
    titleRow.appendChild(idSpan);
    info.appendChild(titleRow);

    const actions = document.createElement('div');
    actions.className = 'attached-item__actions';
    // No manual select: the AI chooses its target via browser_select_page.
    // Active tab is identified by the highlighted background + "#tabId".
    const detachBtn = document.createElement('button');
    detachBtn.className = 'chip chip--small chip--danger';
    detachBtn.title = 'Detach';
    detachBtn.innerHTML = '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>';
    detachBtn.addEventListener('click', () => {
      port.postMessage({ action: 'detachPage', tabId: tab.tabId });
    });
    actions.appendChild(detachBtn);

    row.appendChild(info);
    row.appendChild(actions);
    attachedList.appendChild(row);
  });
}

function updateUI(state) {
  currentState = state;

  // Update status badge.
  statusBadge.textContent = state.status;
  statusBadge.className = 'status-badge';
  if (state.status === 'authorized') {
    statusBadge.classList.add('authorized');
    statusDetail.textContent = state.warning || displayAddr(state.addr) || 'Authorized';
  } else if (state.status === 'connected') {
    statusBadge.classList.add('connected');
    statusDetail.textContent = displayAddr(state.addr) || 'Waiting for attach...';
  } else if (state.status === 'connecting') {
    statusBadge.classList.add('connected');
    statusDetail.textContent = 'Connecting...';
  } else if (state.status === 'error') {
    statusBadge.classList.add('error');
    statusDetail.textContent = state.error || 'Connection error';
  } else {
    statusDetail.textContent = 'Not connected';
  }

  // Attach button: shown when authorized. Attaching is explicit — the user
  // decides when the AI may see a tab.
  attachBtn.style.display = state.status === 'authorized' ? 'block' : 'none';

  // Attached tabs list.
  renderAttachedTabs(state.attachedTabs);
}

// Strip the ws:// scheme for display — the address reads cleaner without it.
function displayAddr(addr) {
  return (addr || '').replace(/^ws:\/\//, '');
}

// ── Event handlers ─────────────────────────────────────────────────────────

port.onMessage.addListener(updateUI);

attachBtn.addEventListener('click', () => {
  port.postMessage({ action: 'attach' });
});

settingsBtn.addEventListener('click', (e) => {
  e.preventDefault();
  if (chrome.runtime.openOptionsPage) {
    chrome.runtime.openOptionsPage();
  }
});

reconnectBtn.addEventListener('click', () => {
  port.postMessage({ action: 'reconnect' });
});

// "Allow AI to attach tabs": remote browser_attach_page requires this explicit
// opt-in (privacy by default — the AI can only operate tabs the user attached
// in the popup). The preference lives in chrome.storage.local, which the
// background service worker reads before honoring attach_page.
const allowRemoteAttach = document.getElementById('allowRemoteAttach');
chrome.storage.local.get(['allowRemoteAttach'], (data) => {
  allowRemoteAttach.checked = !!data.allowRemoteAttach;
});
allowRemoteAttach.addEventListener('change', () => {
  chrome.storage.local.set({ allowRemoteAttach: allowRemoteAttach.checked });
});

// Disable "Attach current tab" when the active page is browser-internal
// (chrome://, chrome-extension://, edge://, ...) — the debugger cannot attach
// to those pages, so clicking would be a dead action.
function refreshAttachBtn() {
  chrome.tabs.query({ active: true, currentWindow: true }, (tabs) => {
    const url = tabs && tabs[0] ? tabs[0].url : '';
    const attachable = isAttachableUrl(url);
    attachBtn.disabled = !attachable;
    attachBtn.title = attachable ? '' : 'Cannot attach browser-internal pages';
  });
}

function isAttachableUrl(url) {
  if (!url) return false;
  return !/^(chrome|chrome-extension|edge|about|devtools|view-source):/i.test(url);
}

// Request initial state.
port.postMessage({ action: 'getState' });

// The active tab cannot change while the popup is open, so a single check on
// open is enough.
refreshAttachBtn();