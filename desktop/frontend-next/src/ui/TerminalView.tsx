import { useCallback, useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import type { ShellChoice, TerminalInfo, TerminalStream } from "../port/terminal";
import { monoFamily, terminalTheme } from "./terminal_theme";
import { noteSeen, seeNote, setTerminalSize, terminalSize, TERMINAL_SIZES } from "./terminal_prefs";
import { StudioIcon } from "./StudioIcon";
import { TerminalHead } from "./TerminalHead";

const bytes = (b64: string) => Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
const RECONNECT_MS = 1000;
const RECONNECTS = 5;

export interface TerminalViewProps {
  port: AgentPort;
  info: TerminalInfo;
  shown: boolean;
  scheme: "light" | "dark";
  shells: ShellChoice[];
  onLoadShells: () => void;
  onShell: (kind: string) => void;
  onQuote: (text: string) => void;
  onOpenPage: (url: string) => void;
  onCommand: (line: string) => void;
  onRestart: () => Promise<void>;
  onClose: () => void;
}

interface Float { x: number; y: number; text: string; url?: string }

// One shell's screen. It stays mounted while another tab is in front, so what
// was on it is still there; the kernel's own copy is what a reader that left
// comes back to.
export function TerminalView({ port, info, shown, scheme, shells, onLoadShells, onShell, onQuote, onOpenPage, onCommand, onRestart, onClose }: TerminalViewProps) {
  const body = useRef<HTMLDivElement>(null);
  const screen = useRef<HTMLDivElement>(null);
  const term = useRef<Terminal | null>(null);
  const fit = useRef<FitAddon | null>(null);
  const stream = useRef<TerminalStream | null>(null);
  const watch = useRef<ResizeObserver | null>(null);
  const typed = useRef("");
  const [exit, setExit] = useState<number | null>(null);
  const [note, setNote] = useState(() => !noteSeen());
  const [float, setFloat] = useState<Float | null>(null);
  const [failure, setFailure] = useState("");
  const [size, setSize] = useState(terminalSize);
  const [round, setRound] = useState(0);
  const latest = useRef({ onCommand, onOpenPage });
  latest.current = { onCommand, onOpenPage };

  const ended = exit ?? (info.running ? null : info.exitCode ?? 0);

  const dropFloat = useCallback(() => setFloat(null), []);

  // The screen is made the first time the tab is in front: it cannot be fitted
  // while hidden, and a terminal made at the wrong size repaints on the first resize.
  useEffect(() => {
    if (!shown || term.current || !screen.current) return;
    const host = screen.current;
    const xterm = new Terminal({
      fontFamily: monoFamily(host), fontSize: terminalSize(), lineHeight: 1.55, cursorBlink: true, scrollback: 5000,
      theme: terminalTheme(host), allowProposedApi: false,
    });
    const fitter = new FitAddon();
    xterm.loadAddon(fitter);
    xterm.loadAddon(new WebLinksAddon((event, url) => {
      const at = body.current?.getBoundingClientRect();
      setFloat({ x: event.clientX - (at?.left ?? 0), y: event.clientY - (at?.top ?? 0), text: url, url });
    }));
    xterm.open(host);
    term.current = xterm;
    fit.current = fitter;
    fitter.fit();
    void port.resizeTerminal(info.id, xterm.cols, xterm.rows).catch(() => {});
    xterm.onData((data) => {
      void port.terminalInput(info.id, data).catch(() => {});
      if (data.startsWith("\x1b")) return;
      for (const ch of data) {
        if (ch === "\r") {
          const line = typed.current.trim();
          typed.current = "";
          if (line) latest.current.onCommand(line);
        } else if (ch === "\x7f") typed.current = typed.current.slice(0, -1);
        else if (ch >= " ") typed.current += ch;
      }
    });
    xterm.onResize(({ cols, rows }) => void port.resizeTerminal(info.id, cols, rows).catch(() => {}));
    xterm.onSelectionChange(() => { if (!xterm.hasSelection()) setFloat((f) => (f?.url ? f : null)); });
    xterm.attachCustomKeyEventHandler((e) => {
      if (e.type === "keydown" && (e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "c" && xterm.hasSelection()) {
        void navigator.clipboard.writeText(xterm.getSelection());
        return false;
      }
      return true;
    });
    watch.current = new ResizeObserver(() => { if (host.offsetParent) fitter.fit(); });
    watch.current.observe(host);
  }, [shown, port, info.id]);

  useEffect(() => () => {
    stream.current?.close();
    watch.current?.disconnect();
    term.current?.dispose();
    term.current = null;
  }, []);

  useEffect(() => {
    const xterm = term.current;
    if (!xterm || !screen.current) return;
    xterm.options.theme = terminalTheme(screen.current);
  }, [scheme]);

  useEffect(() => {
    const xterm = term.current;
    if (!xterm) return;
    xterm.options.fontSize = size;
    fit.current?.fit();
  }, [size, shown]);

  // Reading starts when the tab is in front and stops when it is not, so a
  // closed-over tab holds no connection; coming back reads the screen again from
  // what the kernel kept, which is why nothing is lost by leaving.
  useEffect(() => {
    const xterm = term.current;
    if (!shown || !xterm) return;
    let live = true;
    let attempts = 0;
    let timer = 0;
    const open = () => {
      stream.current = port.streamTerminal(
        info.id,
        (frame) => {
          if (!live) return;
          if (frame.kind === "replay") { xterm.reset(); attempts = 0; if (frame.data) xterm.write(bytes(frame.data)); }
          else if (frame.kind === "out" && frame.data) xterm.write(bytes(frame.data));
          else if (frame.kind === "exit") { attempts = RECONNECTS; setExit(frame.code ?? 0); }
        },
        () => {
          if (!live || attempts >= RECONNECTS) return;
          attempts += 1;
          timer = window.setTimeout(() => { if (live) open(); }, RECONNECT_MS);
        },
      );
    };
    open();
    xterm.focus();
    return () => {
      live = false;
      window.clearTimeout(timer);
      stream.current?.close();
      stream.current = null;
    };
  }, [shown, port, info.id, round]);

  const restart = async () => {
    setFailure("");
    try {
      await onRestart();
      setExit(null);
      setRound((n) => n + 1);
    } catch (e) {
      setFailure(reason(e));
    }
  };

  const copyAll = () => {
    const buffer = term.current?.buffer.active;
    if (!buffer) return;
    const lines: string[] = [];
    for (let i = 0; i < buffer.length; i++) lines.push(buffer.getLine(i)?.translateToString(true) ?? "");
    void navigator.clipboard.writeText(lines.join("\n").replace(/\n+$/, ""));
  };

  const changeSize = (change: "larger" | "smaller" | "reset") => {
    const next = change === "reset" ? TERMINAL_SIZES.standard : size + (change === "larger" ? 1 : -1);
    setTerminalSize(next);
    setSize(terminalSize());
  };

  const pointed = (e: { clientX: number; clientY: number }, text: string) => {
    const at = body.current?.getBoundingClientRect();
    setFloat({ x: e.clientX - (at?.left ?? 0), y: e.clientY - (at?.top ?? 0), text });
  };

  return (
    <div className="term" hidden={!shown} data-scheme={scheme} aria-label={t("terminal.name")}>
      {note && (
        <div className="term-note" role="note">
          <StudioIcon name="shield" />
          <p>{t("terminal.not_the_models")}</p>
          <button type="button" data-action="terminal.note-dismiss" onClick={() => { seeNote(); setNote(false); }}>{t("common.got_it")}</button>
        </div>
      )}
      <TerminalHead
        shell={shells.find((s) => s.path === info.shell)?.kind ?? info.name} dir={info.dir} shells={shells} onLoadShells={onLoadShells} onShell={onShell}
        onClear={() => term.current?.clear()} onReconnect={() => setRound((n) => n + 1)} onCopyAll={copyAll} onSize={changeSize}
      />
      <div className="term-body" ref={body}
        data-action-mouseup="terminal.select" data-action-mousedown="layer.dismiss" data-action-keydown="layer.dismiss"
        onMouseUp={(e) => {
          if ((e.target as HTMLElement).closest(".term-float")) return;
          const text = term.current?.hasSelection() ? term.current.getSelection() : "";
          if (text.trim()) pointed(e, text);
        }}
        onMouseDown={(e) => { if (float && !(e.target as HTMLElement).closest(".term-float")) setFloat(null); }}
        onKeyDown={dropFloat}>
        <div className="term-screen" ref={screen} role="log" aria-label={t("terminal.the_terminal")} />
        {float && (
          <div className="term-float" role="menu" style={{ left: float.x, top: float.y }}>
            {float.url ? (
              <>
                <button type="button" role="menuitem" data-action="terminal.open-link" onClick={() => { latest.current.onOpenPage(float.url!); setFloat(null); }}>
                  <StudioIcon name="globe" />{t("terminal.open_in_browser")}
                </button>
                <button type="button" role="menuitem" data-action="terminal.copy-link" onClick={() => { void navigator.clipboard.writeText(float.url!); setFloat(null); }}>
                  <StudioIcon name="copy" />{t("terminal.copy_link")}
                </button>
              </>
            ) : (
              <>
                <button type="button" role="menuitem" data-action="terminal.send-to-chat" onClick={() => { onQuote(float.text); term.current?.clearSelection(); setFloat(null); }}>
                  <StudioIcon name="quote" />{t("terminal.send_to_chat")}
                </button>
                <button type="button" role="menuitem" data-action="terminal.copy-selection" onClick={() => { void navigator.clipboard.writeText(float.text); term.current?.clearSelection(); setFloat(null); }}>
                  <StudioIcon name="copy" />{t("common.copy")}
                </button>
              </>
            )}
          </div>
        )}
      </div>
      {failure && <div className="workbench-error" role="alert">{failure}</div>}
      {ended !== null && (
        <div className="term-exit" role="status">
          <span>{t("terminal.exited", { code: ended })}</span>
          <button type="button" data-primary="" data-action="terminal.restart" onClick={() => void restart()}>{t("terminal.restart")}</button>
          <button type="button" data-action="terminal.close" onClick={onClose}>{t("workbench.close_tab")}</button>
        </div>
      )}
    </div>
  );
}
