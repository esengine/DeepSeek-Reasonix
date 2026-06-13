import { useEffect, useRef, useState, useCallback } from "react";
import { Terminal } from "xterm";
import { FitAddon } from "@xterm/addon-fit";
import { SearchAddon } from "@xterm/addon-search";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "xterm/css/xterm.css";
import { Plus, X, Search } from "lucide-react";
import { app } from "../lib/bridge";

interface TerminalTab {
  id: string;
  sessionID: string;
  label: string;
  term: Terminal;
  fit: FitAddon;
  search: SearchAddon;
}

const isMac = typeof navigator !== "undefined" && /Mac/i.test(navigator.platform || "");

export function TerminalPanel({ active, openPathRequest }: { active: boolean; openPathRequest?: { id: number; path: string } | null }) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [tabs, setTabs] = useState<TerminalTab[]>([]);
  const [activeTabID, setActiveTabID] = useState<string | null>(null);
  const [searchVisible, setSearchVisible] = useState(false);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const tabsRef = useRef(tabs);
  tabsRef.current = tabs;
  const activeIDRef = useRef(activeTabID);
  activeIDRef.current = activeTabID;
  const startedRef = useRef(false);
  const unsubRef = useRef<(() => void) | null>(null);
  const activeSearchRef = useRef<SearchAddon | null>(null);

  const getTheme = useCallback(() => {
    const style = getComputedStyle(document.documentElement);
    const bg = style.getPropertyValue("--bg").trim() || "#090a0c";
    const fg = style.getPropertyValue("--fg").trim() || "#f4f5f7";
    const accent = style.getPropertyValue("--accent").trim() || "#d97757";
    const dimFg = style.getPropertyValue("--fg-dim").trim() || "#8a8fa0";
    const brightFg = style.getPropertyValue("--fg-bright").trim() || "#ffffff";
    return { bg, fg, accent, dimFg, brightFg };
  }, []);

  const createTab = useCallback(async (label: string, sessionID: string) => {
    const t = getTheme();
    const term = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: "Menlo, Monaco, 'Courier New', monospace",
      theme: {
        background: t.bg,
        foreground: t.fg,
        cursor: t.accent,
        selectionBackground: t.accent + "44",
        black: t.bg, red: "#ff6b6b", green: "#69db7c", yellow: "#ffd43b",
        blue: "#74c0fc", magenta: "#da77f2", cyan: "#63e6be", white: t.fg,
        brightBlack: t.dimFg, brightRed: "#ff8787", brightGreen: "#8ce99a",
        brightYellow: "#ffe066", brightBlue: "#91d5ff", brightMagenta: "#e599f7",
        brightCyan: "#8ce9d0", brightWhite: t.brightFg,
      },
      allowProposedApi: true,
      macOptionIsMeta: true,
    });
    const fit = new FitAddon();
    const search = new SearchAddon();
    const webLinks = new WebLinksAddon();
    term.loadAddon(fit);
    term.loadAddon(search);
    term.loadAddon(webLinks);

    // Copy: Cmd+C / Ctrl+Shift+C → clipboard
    term.attachCustomKeyEventHandler((e: KeyboardEvent) => {
      const mod = isMac ? e.metaKey : e.ctrlKey;
      if (mod && e.key === "c" && term.hasSelection()) {
        const text = term.getSelection();
        navigator.clipboard?.writeText(text);
        return false; // prevent default
      }
      if (mod && e.shiftKey && e.key === "C") {
        const text = term.getSelection();
        if (text) navigator.clipboard?.writeText(text);
        return false;
      }
      // Paste: Cmd+V / Ctrl+Shift+V
      if (mod && e.key === "v") {
        navigator.clipboard?.readText().then((text) => {
          if (text) app.TerminalInput(sessionID, text).catch(() => {});
        });
        return false;
      }
      if (mod && e.shiftKey && e.key === "V") {
        navigator.clipboard?.readText().then((text) => {
          if (text) app.TerminalInput(sessionID, text).catch(() => {});
        });
        return false;
      }
      return true;
    });

    const newTab: TerminalTab = { id: `tab-${Date.now()}`, sessionID, label, term, fit, search };
    setTabs((prev) => {
      const next = [...prev, newTab];
      setActiveTabID(newTab.id);
      return next;
    });
    return newTab;
  }, [getTheme]);

  const closeTab = useCallback((tabID: string) => {
    setTabs((prev) => {
      const tab = prev.find((t) => t.id === tabID);
      if (tab) {
        app.StopTerminal(tab.sessionID).catch(() => {});
        tab.term.dispose();
      }
      const next = prev.filter((t) => t.id !== tabID);
      if (next.length === 0) {
        app.StartTerminal().then((result) => {
          if (result && !result.startsWith("failed:")) {
            createTab("Terminal", result).catch(() => {});
          }
        });
        setActiveTabID(null);
      } else if (activeIDRef.current === tabID) {
        const idx = prev.findIndex((t) => t.id === tabID);
        const newActive = next[Math.min(idx, next.length - 1)];
        setActiveTabID(newActive?.id ?? null);
      }
      return next;
    });
  }, [createTab]);

  // Search
  const toggleSearch = useCallback(() => {
    setSearchVisible((v) => {
      if (!v) {
        setTimeout(() => searchInputRef.current?.focus(), 50);
      }
      return !v;
    });
  }, []);

  const doSearch = useCallback((query: string) => {
    const addon = activeSearchRef.current;
    if (!addon) return;
    if (query) addon.findNext(query, { incremental: true });
  }, []);

  // Resize observer
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    let raf = 0;
    let lastCols = 0;
    let lastRows = 0;
    const observer = new ResizeObserver(() => {
      if (!active) return;
      cancelAnimationFrame(raf);
      raf = requestAnimationFrame(() => {
        const activeTab = tabsRef.current.find((t) => t.id === activeIDRef.current);
        if (!activeTab) return;
        activeTab.fit.fit();
        const { cols, rows } = activeTab.term;
        if (cols !== lastCols || rows !== lastRows) {
          lastCols = cols;
          lastRows = rows;
          app.TerminalResize(activeTab.sessionID, cols, rows).catch(() => {});
        }
      });
    });
    observer.observe(el);
    return () => { observer.disconnect(); cancelAnimationFrame(raf); };
  }, [active]);

  // Switch terminal DOM
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    while (el.firstChild) el.removeChild(el.firstChild);
    const tab = tabs.find((t) => t.id === activeTabID);
    if (tab) {
      tab.term.open(el);
      requestAnimationFrame(() => { tab.fit.fit(); tab.term.focus(); });
      activeSearchRef.current = tab.search;
    }
  }, [tabs, activeTabID]);

  // Open-path requests
  const lastReqRef = useRef<number | null>(null);
  useEffect(() => {
    if (!openPathRequest || openPathRequest.id === lastReqRef.current) return;
    lastReqRef.current = openPathRequest.id;
    app.StartTerminalAt(openPathRequest.path).then((result) => {
      if (result && !result.startsWith("failed:")) {
        const label = openPathRequest.path.split("/").filter(Boolean).pop() || "shell";
        createTab(label, result).catch(() => {});
      }
    }).catch(() => {});
  }, [openPathRequest, createTab]);

  // Start first terminal
  useEffect(() => {
    if (active && !startedRef.current) {
      startedRef.current = true;
      app.StartTerminal().then((result) => {
        if (result && !result.startsWith("failed:") && !result.startsWith("no workspace:")) {
          createTab("Terminal", result).catch(() => {});
        }
      }).catch(() => {});

      if (typeof window !== "undefined" && window.runtime?.EventsOn) {
        unsubRef.current = window.runtime.EventsOn("terminal:output", (raw: unknown) => {
          try {
            const parsed = typeof raw === "string" ? JSON.parse(raw) as { sessionId: string; data: string } : null;
            if (parsed?.data) {
              const match = tabsRef.current.find((t) => t.sessionID === parsed.sessionId);
              if (match) match.term.write(parsed.data);
            }
          } catch { /* not JSON */ }
        });
      }
    }
    return () => { unsubRef.current?.(); };
  }, [active, createTab]);

  // Keyboard input
  useEffect(() => {
    const disposers: Array<{ dispose: () => void }> = [];
    for (const tab of tabs) {
      disposers.push(tab.term.onData((data: string) => {
        if (tab.id === activeIDRef.current) {
          app.TerminalInput(tab.sessionID, data).catch(() => {});
        }
      }));
    }
    return () => { for (const d of disposers) d.dispose(); };
  }, [tabs]);

  const handleNewTab = async () => {
    const id = await app.StartTerminal().catch(() => "");
    if (id && !id.startsWith("failed:") && !id.startsWith("no workspace:")) {
      createTab("Terminal", id).catch(() => {});
    }
  };

  // Keyboard shortcuts: Cmd+T, Cmd+W, Ctrl+Tab
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = isMac ? e.metaKey : e.ctrlKey;
      if (!mod || !active) return;

      if (e.key === "t") { e.preventDefault(); handleNewTab(); return; }
      if (e.key === "w") { e.preventDefault(); const id = activeIDRef.current; if (id) closeTab(id); return; }
      if (e.key === "f") { e.preventDefault(); toggleSearch(); return; }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [active, closeTab]);

  // Tab cycling with Ctrl+Tab / Ctrl+Shift+Tab
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!e.ctrlKey || e.key !== "Tab" || !active || tabs.length < 2) return;
      e.preventDefault();
      const ids = tabs.map((t) => t.id);
      const idx = ids.indexOf(activeIDRef.current ?? "");
      if (e.shiftKey) {
        setActiveTabID(ids[(idx - 1 + ids.length) % ids.length]);
      } else {
        setActiveTabID(ids[(idx + 1) % ids.length]);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [active, tabs]);

  return (
    <div className="terminal-panel">
      <div className="terminal-tabs">
        {tabs.map((tab) => (
          <button
            key={tab.id}
            className={`terminal-tab${tab.id === activeTabID ? " terminal-tab--active" : ""}`}
            onClick={() => { setActiveTabID(tab.id); }}
          >
            <span className="terminal-tab__label">{tab.label}</span>
            <span
              className="terminal-tab__close"
              onClick={(e) => { e.stopPropagation(); closeTab(tab.id); }}
            >
              <X size={10} />
            </span>
          </button>
        ))}
        <button className="terminal-tab terminal-tab--new" onClick={handleNewTab} title="New terminal (Cmd+T)">
          <Plus size={12} />
        </button>
        <span className="terminal-tabs__spacer" />
        <button className="terminal-tab terminal-tab--icon" onClick={toggleSearch} title="Search (Cmd+F)">
          <Search size={13} />
        </button>
      </div>
      {searchVisible && (
        <div className="terminal-search">
          <input
            ref={searchInputRef}
            className="terminal-search__input"
            placeholder="Find…"
            onChange={(e) => doSearch(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape") { setSearchVisible(false); e.currentTarget.value = ""; }
              if (e.key === "Enter") doSearch(e.currentTarget.value);
            }}
          />
        </div>
      )}
      <div className="terminal-panel__body" ref={containerRef} />
    </div>
  );
}
