// Chrome renderer script: wires the toolbar DOM to the minimal bridge. Runs
// only inside the trusted chrome view; it has no access to remote pages.

// Plain browser script (no module exports): loaded via <script src> in
// chrome.html. The bridge type is declared locally instead of on the global
// Window so the compiled output stays a plain script.

interface ReasonixChromeBridge {
  command(cmd: {
    kind: "activateTab" | "closeTab" | "newTab" | "navigate" | "back" | "forward" | "reload" | "takeover";
    tabId?: string;
    url?: string;
  }): void;
  onState(cb: (state: {
    tabs: Array<{ id: string; url: string; title: string; active: boolean }>;
    agentControlling: boolean;
  }) => void): () => void;
  requestState(): void;
}

// The bridge property (window.reasonixChrome) is injected by the preload via
// contextBridge. The local name must NOT collide with the injected global
// property: a top-level let/const with the same name throws
// "Identifier has already been declared" in the page.
const chromeBridge = (window as unknown as { reasonixChrome?: ReasonixChromeBridge }).reasonixChrome;

const address = document.getElementById("address") as HTMLInputElement;
const tabstrip = document.getElementById("tabstrip") as HTMLElement;
const agentBadge = document.getElementById("agent-badge") as HTMLElement;
const takeover = document.getElementById("takeover") as HTMLButtonElement;

function bind(id: string, kind: "back" | "forward" | "reload" | "newTab" | "takeover", fn?: () => void): void {
  const el = document.getElementById(id);
  el?.addEventListener("click", () => {
    if (fn) fn();
    chromeBridge!.command({ kind });
  });
}

bind("back", "back");
bind("forward", "forward");
bind("reload", "reload");
bind("new-tab", "newTab");
bind("takeover", "takeover");

address.addEventListener("keydown", (event) => {
  if (event.key === "Enter") {
    const url = address.value.trim();
    if (!url) return;
    chromeBridge!.command({ kind: "navigate", url });
  }
  if (event.key === "Escape") {
    address.blur();
  }
});

document.addEventListener("keydown", (event) => {
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "l") {
    event.preventDefault();
    address.focus();
    address.select();
  }
});

tabstrip.addEventListener("click", (event) => {
  const tab = (event.target as HTMLElement).closest<HTMLElement>(".tab");
  if (!tab) return;
  const tabId = tab.dataset.tabId;
  if (!tabId) return;
  if ((event.target as HTMLElement).closest(".tab-close")) {
    chromeBridge!.command({ kind: "closeTab", tabId });
  } else {
    chromeBridge!.command({ kind: "activateTab", tabId });
  }
});

const newTabBtn = document.createElement("button");
newTabBtn.className = "tab-new";
newTabBtn.textContent = "+";
newTabBtn.title = "New tab";
newTabBtn.setAttribute("aria-label", "New tab");
newTabBtn.addEventListener("click", () => chromeBridge!.command({ kind: "newTab" }));
tabstrip.appendChild(newTabBtn);

interface ChromeState {
  tabs: Array<{ id: string; url: string; title: string; active: boolean }>;
  agentControlling: boolean;
}

chromeBridge!.onState((state: ChromeState) => {
  // Rebuild the tab strip (small counts; simpler than diffing).
  for (const el of [...tabstrip.querySelectorAll(".tab")]) {
    el.remove();
  }
  for (const tab of state.tabs) {
    const el = document.createElement("div");
    el.className = "tab";
    el.dataset.tabId = tab.id;
    el.setAttribute("role", "tab");
    el.setAttribute("data-active", String(tab.active));
    const title = document.createElement("span");
    title.className = "tab-title";
    title.textContent = tab.title || tab.url || "New tab";
    title.title = tab.url;
    const close = document.createElement("button");
    close.className = "tab-close";
    close.textContent = "×";
    close.setAttribute("aria-label", "Close tab");
    el.append(title, close);
    tabstrip.insertBefore(el, newTabBtn);
  }
  agentBadge.classList.toggle("hidden", !state.agentControlling);
  takeover.classList.toggle("hidden", !state.agentControlling);
  if (!state.agentControlling) {
    agentBadge.textContent = "";
  } else {
    agentBadge.textContent = "Agent";
  }
});

chromeBridge!.requestState();
