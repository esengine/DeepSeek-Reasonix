import { useRef, useState, type Dispatch, type SetStateAction } from "react";
import type { AgentPort } from "../port/port";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { keyOf, labelOf, type Surface } from "./workbench_tabs";
import type { RunTab } from "./useAgentTabs";

type Setter<T> = Dispatch<SetStateAction<T>>;
interface Props {
  port: AgentPort; surfaces: Surface[]; selected: string; showFiles: boolean;
  setBrowsers: Setter<string[]>; setOpenFiles: Setter<string[]>; setAgents: Setter<RunTab[]>; setDismissed: Setter<Set<string>>;
  setSelected: Setter<string>; onCloseManual: () => void; onCloseSelected: () => void;
  setLauncher?: Setter<boolean>;
}

export function useTabClose({ port, surfaces, selected, showFiles, setBrowsers, setOpenFiles, setAgents, setDismissed, setSelected, onCloseManual, onCloseSelected, setLauncher }: Props) {
  const current = useRef({ surfaces, selected, showFiles });
  current.current = { surfaces, selected, showFiles };
  const pending = useRef(false);
  const [closing, setClosing] = useState(false);
  const [closeFailure, setCloseFailure] = useState("");
  const close = async (targets: Surface[]) => {
    if (pending.current) return;
    pending.current = true;
    setClosing(true);
    const closed = new Set<string>();
    const failures: string[] = [];
    try {
      for (const surface of targets) {
        try {
          if (surface.kind === "browser") await port.browserClose(surface.tab.id);
          else if (surface.kind === "terminal") await port.closeTerminal(surface.id);
          closed.add(keyOf(surface));
        } catch (e) {
          failures.push(t("workbench.could_not_close_name_why", { name: labelOf(surface, {}), why: reason(e) }));
        }
      }
      setBrowsers((v) => v.filter((id) => !closed.has(`manual:${id}`)));
      setOpenFiles((v) => v.filter((path) => !closed.has(`file:${path}`)));
      setAgents((v) => v.filter((run) => !closed.has(keyOf(run))));
      setDismissed((v) => new Set([...v, ...closed]));
      if (closed.has(current.current.selected)) onCloseSelected();
      setSelected((v) => closed.has(v) ? "" : v);
      setCloseFailure(failures.join("\n"));
      // Closing the last page with no list open leaves the column with something
      // to offer rather than folding it away.
      if (!current.current.showFiles && current.current.surfaces.every((s) => closed.has(keyOf(s)))) {
        if (targets.some((s) => s.kind !== "file")) {
          setLauncher?.(true);
        } else {
          onCloseManual();
        }
      }
    } finally { pending.current = false; setClosing(false); }
  };
  return { close, closing, closeFailure };
}
