import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { RefreshCw, TerminalSquare } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

import { app, onTerminalData, onTerminalExit, type TerminalDataEvent } from "../lib/bridge";
import { useT } from "../lib/i18n";

interface TerminalPanelProps {
  tabId: string;
}

type TerminalStatus = "starting" | "running" | "exited" | "error";

function decodeBase64(value: string): Uint8Array {
  if (!value) return new Uint8Array();
  const binary = window.atob(value);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);
  return bytes;
}

function encodeBase64(value: string): string {
  const bytes = new TextEncoder().encode(value);
  let binary = "";
  const chunkSize = 0x8000;
  for (let offset = 0; offset < bytes.length; offset += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + chunkSize));
  }
  return window.btoa(binary);
}

function terminalTheme() {
  const style = getComputedStyle(document.documentElement);
  const color = (name: string, fallback: string) => style.getPropertyValue(name).trim() || fallback;
  return {
    background: color("--bg", "#0d0f14"),
    foreground: color("--fg", "#e8eaf0"),
    cursor: color("--accent", "#f0784a"),
    cursorAccent: color("--bg", "#0d0f14"),
    selectionBackground: color("--selection", "#3b4252"),
    black: "#1b1d23",
    red: "#e06c75",
    green: "#98c379",
    yellow: "#e5c07b",
    blue: "#61afef",
    magenta: "#c678dd",
    cyan: "#56b6c2",
    white: "#d7dae0",
    brightBlack: "#6b7280",
    brightRed: "#ff7a85",
    brightGreen: "#b2dc8b",
    brightYellow: "#f3d38b",
    brightBlue: "#7fc1ff",
    brightMagenta: "#dd91f3",
    brightCyan: "#75d4df",
    brightWhite: "#ffffff",
  };
}

export function TerminalPanel({ tabId }: TerminalPanelProps) {
  const t = useT();
  const hostRef = useRef<HTMLDivElement>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const sessionRef = useRef("");
  const sequenceRef = useRef(0);
  const pendingRef = useRef<TerminalDataEvent[]>([]);
  const startGenerationRef = useRef(0);
  const [status, setStatus] = useState<TerminalStatus>("starting");
  const [detail, setDetail] = useState("");
  const [cwd, setCwd] = useState("");

  const fitAndResize = useCallback(() => {
    const terminal = terminalRef.current;
    const fit = fitRef.current;
    if (!terminal || !fit || !hostRef.current?.isConnected) return;
    const rect = hostRef.current.getBoundingClientRect();
    if (rect.width < 2 || rect.height < 2) return;
    try {
      fit.fit();
    } catch {
      return;
    }
    const sessionID = sessionRef.current;
    if (sessionID) {
      void app.TerminalResize(tabId, sessionID, terminal.cols, terminal.rows).catch(() => {});
    }
  }, [tabId]);

  const start = useCallback(async (reset = false) => {
    const terminal = terminalRef.current;
    const fit = fitRef.current;
    if (!terminal || !fit) return;
    const generation = ++startGenerationRef.current;
    setStatus("starting");
    setDetail("");
    if (reset) {
      const current = sessionRef.current;
      sessionRef.current = "";
      sequenceRef.current = 0;
      pendingRef.current = [];
      terminal.reset();
      if (current) await app.TerminalClose(tabId, current).catch(() => {});
    }
    fitAndResize();
    try {
      const view = await app.TerminalStart(tabId, terminal.cols, terminal.rows);
      if (generation !== startGenerationRef.current) return;
      sessionRef.current = view.sessionId;
      sequenceRef.current = view.sequence || 0;
      setCwd(view.cwd);
      if (view.snapshot) terminal.write(decodeBase64(view.snapshot));
      if (view.mock) terminal.writeln(`\r\n${t("terminal.browserOnly")}\r\n`);
      const pending = pendingRef.current
        .filter((event) => event.sessionId === view.sessionId && event.sequence > sequenceRef.current)
        .sort((a, b) => a.sequence - b.sequence);
      pendingRef.current = [];
      for (const event of pending) {
        terminal.write(decodeBase64(event.data));
        sequenceRef.current = Math.max(sequenceRef.current, event.sequence);
      }
      setStatus("running");
      window.requestAnimationFrame(() => {
        fitAndResize();
        terminal.focus();
      });
    } catch (error) {
      if (generation !== startGenerationRef.current) return;
      setStatus("error");
      setDetail(error instanceof Error ? error.message : String(error));
    }
  }, [fitAndResize, t, tabId]);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    const terminal = new Terminal({
      allowProposedApi: false,
      convertEol: false,
      cursorBlink: true,
      cursorStyle: "bar",
      fontFamily: "Cascadia Code, JetBrains Mono, Consolas, monospace",
      fontSize: 13,
      lineHeight: 1.18,
      scrollback: 10_000,
      theme: terminalTheme(),
    });
    const fit = new FitAddon();
    terminal.loadAddon(fit);
    terminal.open(host);
    terminalRef.current = terminal;
    fitRef.current = fit;

    const inputSubscription = terminal.onData((data) => {
      const sessionID = sessionRef.current;
      if (!sessionID) return;
      void app.TerminalWrite(tabId, sessionID, encodeBase64(data)).catch((error) => {
        setStatus("error");
        setDetail(error instanceof Error ? error.message : String(error));
      });
    });
    const resizeObserver = new ResizeObserver(() => fitAndResize());
    resizeObserver.observe(host);
    const themeObserver = new MutationObserver(() => {
      terminal.options.theme = terminalTheme();
    });
    themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ["class", "data-theme", "style"] });

    const unsubscribeData = onTerminalData((event) => {
      if (event.tabId !== tabId) return;
      const sessionID = sessionRef.current;
      if (!sessionID) {
        pendingRef.current.push(event);
        return;
      }
      if (event.sessionId !== sessionID || event.sequence <= sequenceRef.current) return;
      terminal.write(decodeBase64(event.data));
      sequenceRef.current = event.sequence;
    });
    const unsubscribeExit = onTerminalExit((event) => {
      if (event.tabId !== tabId || event.sessionId !== sessionRef.current) return;
      sessionRef.current = "";
      setStatus(event.error ? "error" : "exited");
      setDetail(event.error || "");
    });

    void start();
    return () => {
      startGenerationRef.current += 1;
      inputSubscription.dispose();
      resizeObserver.disconnect();
      themeObserver.disconnect();
      unsubscribeData();
      unsubscribeExit();
      terminal.dispose();
      terminalRef.current = null;
      fitRef.current = null;
    };
  }, [fitAndResize, start, tabId]);

  const statusLabel = status === "running"
    ? t("terminal.running")
    : status === "starting"
      ? t("terminal.starting")
      : status === "exited"
        ? t("terminal.exited")
        : t("terminal.failed");

  return (
    <section className="terminal-panel" aria-label={t("terminal.title")}>
      <header className="terminal-panel__toolbar">
        <div className="terminal-panel__identity" title={cwd}>
          <TerminalSquare size={14} aria-hidden="true" />
          <span>{cwd || t("terminal.title")}</span>
        </div>
        <div className={`terminal-panel__status terminal-panel__status--${status}`}>{statusLabel}</div>
        <button
          type="button"
          className="terminal-panel__action"
          onClick={() => void start(true)}
          disabled={status === "starting"}
          aria-label={t("terminal.restart")}
          title={t("terminal.restart")}
        >
          <RefreshCw size={14} aria-hidden="true" />
        </button>
      </header>
      <div className="terminal-panel__viewport" ref={hostRef} />
      {(status === "error" || status === "exited") && (
        <div className="terminal-panel__overlay" role="status">
          <strong>{statusLabel}</strong>
          {detail && <span>{detail}</span>}
          <button type="button" onClick={() => void start(true)}>{t("terminal.restart")}</button>
        </div>
      )}
    </section>
  );
}