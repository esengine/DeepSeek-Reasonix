import { useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { t } from "../i18n";
import type { ShellChoice } from "../port/terminal";
import { listenAction } from "./listen";
import { TerminalHostContext } from "./terminal_host";
import { StudioIcon } from "./StudioIcon";

// Closes a dropdown when the press lands outside it or Escape is pressed.
function useDismissed(open: boolean, close: () => void) {
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    return listenAction(document, "pointerdown", { action: "layer.dismiss", listener: (e: Event) => { if (!box.current?.contains(e.target as Node)) close(); } });
  }, [open, close]);
  return box;
}

function Drop({ label, button, children, onOpen }: { label: string; button: (toggle: () => void, open: boolean) => ReactNode; children: (close: () => void) => ReactNode; onOpen?: () => void }) {
  const [open, setOpen] = useState(false);
  const close = () => setOpen(false);
  const box = useDismissed(open, close);
  return (
    <div className="term-drop" ref={box} data-action-keydown="layer.dismiss" onKeyDown={(e) => { if (e.key === "Escape" && open) { e.stopPropagation(); close(); } }}>
      {button(() => { if (!open) onOpen?.(); setOpen(!open); }, open)}
      {open && <div className="term-menu" role="menu" aria-label={label}>{children(close)}</div>}
    </div>
  );
}

export interface TerminalHeadProps {
  shell: string;
  dir: string;
  shells: ShellChoice[];
  onLoadShells: () => void;
  onShell: (kind: string) => void;
  onClear: () => void;
  onReconnect: () => void;
  onCopyAll: () => void;
  onSize: (change: "larger" | "smaller" | "reset") => void;
}

// The shell is named here and picked here; choosing another one opens a
// terminal in it beside this one, so nothing running is cut off.
export function TerminalHead({ shell, dir, shells, onLoadShells, onShell, onClear, onReconnect, onCopyAll, onSize }: TerminalHeadProps) {
  const machine = useContext(TerminalHostContext);
  return (
    <div className="term-head">
      <Drop label={t("terminal.pick_a_shell")} onOpen={onLoadShells}
        button={(toggle, open) => (
          <button type="button" className="term-shell" data-action="terminal.shell-menu" aria-haspopup="menu" aria-expanded={open} aria-label={t("terminal.shell_picker")} onClick={toggle}>
            {shell}<StudioIcon name="down" />
          </button>
        )}>
        {(close) => (
          <>
            {shells.map((choice) => (
              <button key={choice.kind} type="button" role="menuitem" data-action="terminal.shell" data-target={choice.kind} data-current={choice.kind === shell ? "" : undefined}
                onClick={() => { close(); if (choice.kind !== shell) onShell(choice.kind); }}>
                <span>{choice.name}</span><kbd>{choice.kind}</kbd>{choice.kind === shell && <StudioIcon name="check" />}
              </button>
            ))}
            <p>{t("terminal.default_shell_note")}</p>
          </>
        )}
      </Drop>
      {machine && <span className="term-host"><StudioIcon name="server" />{t("terminal.on_host", { host: machine })}</span>}
      <span className="term-dir" title={dir}>{dir}</span>
      <span className="term-grow" />
      <button type="button" className="term-tool" data-action="terminal.clear" aria-label={t("terminal.clear")} title={t("terminal.clear")} onClick={onClear}><StudioIcon name="eraser" /></button>
      <button type="button" className="term-tool" data-action="terminal.reconnect" aria-label={t("terminal.reconnect")} title={t("terminal.reconnect")} onClick={onReconnect}><StudioIcon name="refresh" /></button>
      <Drop label={t("terminal.more")}
        button={(toggle, open) => (
          <button type="button" className="term-tool" data-action="terminal.more" aria-haspopup="menu" aria-expanded={open} aria-label={t("terminal.more")} title={t("terminal.more")} onClick={toggle}><StudioIcon name="more" /></button>
        )}>
        {(close) => (
          <>
            <button type="button" role="menuitem" data-action="terminal.copy-all" onClick={() => { close(); onCopyAll(); }}><span>{t("terminal.copy_all")}</span></button>
            <button type="button" role="menuitem" data-action="terminal.size" data-value="larger" onClick={() => onSize("larger")}><span>{t("terminal.font_larger")}</span></button>
            <button type="button" role="menuitem" data-action="terminal.size" data-value="smaller" onClick={() => onSize("smaller")}><span>{t("terminal.font_smaller")}</span></button>
            <button type="button" role="menuitem" data-action="terminal.size" data-value="reset" onClick={() => { close(); onSize("reset"); }}><span>{t("terminal.font_reset")}</span></button>
          </>
        )}
      </Drop>
    </div>
  );
}
