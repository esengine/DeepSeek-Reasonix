import { useEffect, useRef, useState, useCallback } from "react";
import { Terminal } from "xterm";
import { FitAddon } from "@xterm/addon-fit";
import "xterm/css/xterm.css";
import { Plus, X } from "lucide-react";
import { app } from "../lib/bridge";

interface TerminalTab {
  id: string;
  sessionID: string;
  label: string;
  term: Terminal;
  fit: FitAddon;
}

export function TerminalPanel({ active, openPathRequest }: { active: boolean; openPathRequest?: { id: number; path: string } | null }) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [tabs, setTabs] = useState<TerminalTab[]>([]);
  const [activeTabID, setActiveTabID] = useState<string | null>(null);
  const tabsRef = useRef(tabs);
  tabsRef.current = tabs;
  const activeIDRef = useRef(activeTabID);
  activeIDRef.current = activeTabID;
  const startedRef = useRef(false);
  const unsubRef = useRef<(() => void) | null>(null);

  const createTab = useCallback(async (label: string, sessionID: string) => {
    const term = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: "Menlo, Monaco, 'Courier New', monospace",
      theme: {
        background: "#090a0c",
        foreground: "#c8c8d0",
        cursor: "#c8c8d0",
        selectionBackground: "#2a2d3a",
        black: "#090a0c",
        red: "#ff6b6b",
        green: "#69db7c",
        yellow: "#ffd43b",
        blue: "#74c0fc",
        magenta: "#da77f2",
        cyan: "#63e6be",
        white: "#c8c8d0",
        brightBlack: "#3d3d5c",
        brightRed: "#ff8787",
        brightGreen: "#8ce99a",
        brightYellow: "#ffe066",
        brightBlue: "#91d5ff",
        brightMagenta: "#e599f7",
        brightCyan: "#8ce9d0",
        brightWhite: "#e8e8f0",
      },
      allowProposedApi: true,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);

    const newTab: TerminalTab = { id: `tab-${Date.now()}`, sessionID, label, term, fit };
    setTabs((prev) => {
      const next = [...prev, newTab];
      if (!activeIDRef.current) setActiveTabID(newTab.id);
      return next;
    });

    return newTab;
  }, []);

  const closeTab = useCallback((tabID: string) => {
    setTabs((prev) => {
      const tab = prev.find((t) => t.id === tabID);
      if (tab) {
        void app.StopTerminal(tab.sessionID).catch(() => {});
        tab.term.dispose();
      }
      const next = prev.filter((t) => t.id !== tabID);
      if (activeIDRef.current === tabID) {
        const idx = prev.findIndex((t) => t.id === tabID);
        const newActive = next[Math.min(idx, next.length - 1)];
        setActiveTabID(newActive?.id ?? null);
      }
      return next;
    });
  }, []);

  // Resize observer for active terminal — debounced with rAF.
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    let raf = 0;
    let lastCols = 0;
    let lastRows = 0;

    const observer = new ResizeObserver(() => {
      if (!active) return; // skip when panel hidden
      cancelAnimationFrame(raf);
      raf = requestAnimationFrame(() => {
        const activeTab = tabsRef.current.find((t) => t.id === activeIDRef.current);
        if (!activeTab) return;
        activeTab.fit.fit();
        const { cols, rows } = activeTab.term;
        if (cols !== lastCols || rows !== lastRows) {
          lastCols = cols;
          lastRows = rows;
          void app.TerminalResize(activeTab.sessionID, cols, rows).catch(() => {});
        }
      });
    });
    observer.observe(el);
    return () => {
      observer.disconnect();
      cancelAnimationFrame(raf);
    };
  }, [active]);

  // Switch terminal DOM when active tab changes.
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    // Remove any existing terminal element.
    while (el.firstChild) {
      el.removeChild(el.firstChild);
    }
    // Open the active tab's terminal.
    const tab = tabs.find((t) => t.id === activeTabID);
    if (tab) {
      tab.term.open(el);
      requestAnimationFrame(() => {
        tab.fit.fit();
        tab.term.focus();
      });
    }
  }, [tabs, activeTabID]);

  // Handle external open-path requests.
  const lastReqRef = useRef<number | null>(null);
  useEffect(() => {
    if (!openPathRequest || openPathRequest.id === lastReqRef.current) return;
    lastReqRef.current = openPathRequest.id;
    void app.StartTerminalAt(openPathRequest.path).then((result) => {
      if (result && !result.startsWith("failed:")) {
        const label = openPathRequest.path.split("/").filter(Boolean).pop() || "shell";
        void createTab(label, result);
      }
    });
  }, [openPathRequest, createTab]);

  // Start first terminal + subscribe to output.
  useEffect(() => {
    if (active && !startedRef.current) {
      startedRef.current = true;

      void app.StartTerminal().then((result) => {
        if (result.startsWith("failed:") || result.startsWith("no workspace:")) {
          const tabPromise = createTab("Terminal", "");
          tabPromise.then((tab) => {
            tab.term.write(`\r\n\x1b[31m${result}\x1b[0m\r\n`);
          });
        } else {
          void createTab("Terminal", result);
        }
      });

      // Subscribe to terminal output events.
      if (typeof window !== "undefined" && window.runtime?.EventsOn) {
        unsubRef.current = window.runtime.EventsOn("terminal:output", (raw: unknown) => {
          try {
            const parsed = typeof raw === "string" ? JSON.parse(raw) as { sessionId: string; data: string } : null;
            if (parsed?.data) {
              const match = tabsRef.current.find((t) => t.sessionID === parsed.sessionId);
              if (match) match.term.write(parsed.data);
            }
          } catch {
            // Not JSON — legacy or malformed.
          }
        });
      }
    }

    return () => {
      unsubRef.current?.();
      startedRef.current = false;
      for (const tab of tabs) {
        void app.StopTerminal(tab.sessionID).catch(() => {});
        tab.term.dispose();
      }
    };
  }, [active]);

  // Keyboard input → current tab's PTY.
  useEffect(() => {
    const disposers: Array<{ dispose: () => void }> = [];
    for (const tab of tabs) {
      disposers.push(tab.term.onData((data: string) => {
        if (tab.id === activeIDRef.current) {
          void app.TerminalInput(tab.sessionID, data).catch(() => {});
        }
      }));
    }
    return () => { for (const d of disposers) d.dispose(); };
  }, [tabs]);

  const handleNewTab = async () => {
    const id = await app.StartTerminal();
    if (id && !id.startsWith("failed:") && !id.startsWith("no workspace:")) {
      void createTab("Terminal", id);
    }
  };

  return (
    <div className="terminal-panel">
      <div className="terminal-tabs">
        {tabs.map((tab) => (
          <button
            key={tab.id}
            className={`terminal-tab${tab.id === activeTabID ? " terminal-tab--active" : ""}`}
            onClick={() => setActiveTabID(tab.id)}
          >
            <span className="terminal-tab__label">{tab.label}</span>
            {tabs.length > 1 && (
              <span
                className="terminal-tab__close"
                onClick={(e) => { e.stopPropagation(); closeTab(tab.id); }}
              >
                <X size={10} />
              </span>
            )}
          </button>
        ))}
        <button className="terminal-tab terminal-tab--new" onClick={handleNewTab} title="New terminal">
          <Plus size={12} />
        </button>
      </div>
      <div className="terminal-panel__body" ref={containerRef} />
    </div>
  );
}
