// Reasonix Browser Relay — Options Page

const portInput = document.getElementById('portInput');
const tokenInput = document.getElementById('tokenInput');
const toggleTokenBtn = document.getElementById('toggleTokenBtn');
const saveBtn = document.getElementById('saveBtn');
const saveStatus = document.getElementById('saveStatus');

// ── Token show/hide ────────────────────────────────────────────────────────

toggleTokenBtn.addEventListener('click', () => {
  const isPassword = tokenInput.type === 'password';
  tokenInput.type = isPassword ? 'text' : 'password';
  toggleTokenBtn.textContent = isPassword ? 'Hide' : 'Show';
});

// ── Load saved settings ────────────────────────────────────────────────────

chrome.storage.local.get(['relayPort', 'relayToken'], (data) => {
  // Fresh installs have no stored port; initialize with the documented default
  // (23002) so entering only the token establishes the first connection.
  portInput.value = data.relayPort || '23002';
  if (data.relayToken) tokenInput.value = data.relayToken;
});

// ── Save settings ──────────────────────────────────────────────────────────

saveBtn.addEventListener('click', () => {
  const port = portInput.value.trim();
  const token = tokenInput.value.trim();

  if (!port) {
    showStatus('Port is required', true);
    portInput.focus();
    return;
  }
  if (!/^\d+$/.test(port)) {
    showStatus('Port must be a number', true);
    portInput.focus();
    return;
  }
  if (!token) {
    showStatus('Token is required', true);
    tokenInput.focus();
    return;
  }

  const addr = 'ws://127.0.0.1:' + port;

  chrome.storage.local.set({ relayPort: port, relayAddr: addr, relayToken: token, autoConnect: true }, () => {
    showStatus('Settings saved');
    
    // Notify background to connect with new settings.
    chrome.runtime.sendMessage({ 
      action: 'connect', 
      addr: addr, 
      token: token 
    }).catch(() => {
      // Background might not be listening yet, that's ok.
    });
  });
});

function showStatus(msg, isError = false) {
  saveStatus.textContent = msg;
  saveStatus.className = 'save-status' + (isError ? ' error' : '');
  if (!isError) {
    setTimeout(() => { saveStatus.textContent = ''; }, 3000);
  }
}