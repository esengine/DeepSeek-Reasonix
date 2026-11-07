import { useCallback, useEffect, useMemo, useRef, useState, type Dispatch, type SetStateAction } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { host } from "../port/host";
import type { AgentPort } from "../port/port";
import type { ShellChoice, TerminalInfo } from "../port/terminal";
import { openPage } from "./browser_open";
import { lastShell, rememberShell } from "./terminal_prefs";
import type { Surface } from "./workbench_tabs";

export type TerminalTab = Extract<Surface, { kind: "terminal" }>;

const POLL_MS = 2000;

interface Props {
  port: AgentPort;
  shown: boolean;
  pick: (key: string) => void;
  setDismissed: Dispatch<SetStateAction<Set<string>>>;
  onExternal: (url: string) => void;
  onError: (message: string) => void;
  // Counts the presses that ask for a terminal: the one most recently used, or a first one.
  request: number;
}

/** The person's terminals as workbench tabs. The kernel holds the list, so a
 *  terminal opened from another window, or before this one reloaded, is a tab
 *  here too; a tab is named for the command last submitted while it is busy. */
export function useTerminalTabs({ port, shown, pick, setDismissed, onExternal, onError, request }: Props) {
  const [list, setList] = useState<TerminalInfo[]>([]);
  const [shells, setShells] = useState<ShellChoice[]>([]);
  const [commands, setCommands] = useState<Record<string, string>>({});
  const changed = useRef(0);
  const held = useRef(list);
  held.current = list;

  useEffect(() => {
    if (!shown) return;
    let live = true;
    const read = () => {
      const since = changed.current;
      port.terminals().then((next) => {
        if (!live || since !== changed.current) return;
        // Not even an update that hands back the same value: it costs the
        // component another render, and the poll runs for as long as the panel.
        if (JSON.stringify(held.current) === JSON.stringify(next)) return;
        setList(next);
      }, () => {});
    };
    read();
    const timer = window.setInterval(read, POLL_MS);
    return () => { live = false; window.clearInterval(timer); };
  }, [port, shown]);

  const loadShells = useCallback(() => { port.terminalShells().then(setShells, () => {}); }, [port]);
  useEffect(() => { if (shown) loadShells(); }, [shown, loadShells]);

  const add = useCallback((info: TerminalInfo) => {
    changed.current += 1;
    setList((v) => [...v.filter((x) => x.id !== info.id), info]);
    setDismissed((v) => { const n = new Set(v); n.delete(`terminal:${info.id}`); return n; });
    pick(`terminal:${info.id}`);
  }, [pick, setDismissed]);

  const open = useCallback(async (kind = lastShell()) => {
    try {
      const info = await port.openTerminal(kind, 0, 0);
      add(info);
      const used = kind || shells.find((s) => s.path === info.shell)?.kind;
      if (used) rememberShell(used);
    } catch (e) {
      onError(t("terminal.could_not_open", { why: reason(e) }));
    }
  }, [port, add, shells, onError]);

  const handled = useRef(request);
  useEffect(() => {
    if (request === handled.current) return;
    handled.current = request;
    const latest = list.at(-1);
    if (latest) pick(`terminal:${latest.id}`);
    else void open();
  }, [request, list, pick, open]);

  const restart = useCallback(async (id: string) => {
    const info = await port.restartTerminal(id);
    changed.current += 1;
    setList((v) => v.map((x) => (x.id === id ? info : x)));
  }, [port]);

  const openUrl = useCallback(async (url: string) => {
    if (!host().drawsBrowserViews()) return onExternal(url);
    try {
      pick(`browser:${(await openPage(port, url)).target}`);
    } catch (e) {
      onError(reason(e));
    }
  }, [port, pick, onExternal, onError]);

  const [asking, setAsking] = useState<{ anchor: HTMLElement; run: () => void } | null>(null);
  const cancelAsk = useCallback(() => setAsking(null), []);
  const confirmAsk = useCallback(() => { asking?.run(); setAsking(null); }, [asking]);
  // Closing a terminal with a command still running asks first; whichever way
  // the close was asked for, it goes through here.
  const guard = useCallback((targets: Surface[], run: (targets: Surface[]) => Promise<void>) => {
    const busy = targets.find((s) => s.kind === "terminal" && s.busy);
    if (!busy) return run(targets);
    const anchor = document.querySelector<HTMLElement>(`[data-key="terminal:${busy.kind === "terminal" ? busy.id : ""}"]`) ?? document.body;
    setAsking({ anchor, run: () => void run(targets) });
    return Promise.resolve();
  }, []);

  const submitted = useCallback((id: string, line: string) => setCommands((v) => ({ ...v, [id]: line })), []);

  const tabs = useMemo<TerminalTab[]>(() => list.map((info, i) => ({
    kind: "terminal", id: info.id, busy: info.busy,
    name: info.busy && commands[info.id] ? commands[info.id] : list.length === 1 ? t("terminal.name") : t("terminal.n", { n: i + 1 }),
  })), [list, commands]);

  return { terminals: list, tabs, shells, loadShells, open, restart, openUrl, submitted, asking, cancelAsk, confirmAsk, guard };
}
