import { useRef, useState, type Dispatch, type SetStateAction } from "react";
import type { AgentPort } from "../port/port";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { keyOf, labelOf, type Surface } from "./workbench_tabs";
import type { AgentTab } from "./useAgentTabs";

type Setter<T> = Dispatch<SetStateAction<T>>;
interface Props {
  port: AgentPort; surfaces: Surface[]; selected: string; showFiles: boolean;
  setBrowsers: Setter<string[]>; setOpenFiles: Setter<string[]>; setAgents: Setter<AgentTab[]>; setDismissed: Setter<Set<string>>;
  setSelected: Setter<string>; onCloseManual: () => void; onCloseSelected: () => void;
  fromExplorer?: { current: string }; setShowFiles?: Setter<boolean>; setLauncher?: Setter<boolean>;
}

export function useTabClose({ port, surfaces, selected, showFiles, setBrowsers, setOpenFiles, setAgents, setDismissed, setSelected, onCloseManual, onCloseSelected, fromExplorer, setShowFiles, setLauncher }: Props) {
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
          closed.add(keyOf(surface));
        } catch (e) {
          failures.push(t("无法关闭 {name}：{why}", { name: labelOf(surface, {}), why: reason(e) }));
        }
      }
      setBrowsers((v) => v.filter((id) => !closed.has(`manual:${id}`)));
      setOpenFiles((v) => v.filter((path) => !closed.has(`file:${path}`)));
      setAgents((v) => v.filter((run) => !closed.has(`agent:${run.ref}`)));
      setDismissed((v) => new Set([...v, ...closed]));
      if (closed.has(current.current.selected)) onCloseSelected();
      setSelected((v) => closed.has(v) ? "" : v);
      setCloseFailure(failures.join("\n"));
      // A file picked out of the explorer leaves that list stepped aside, not
      // closed. When that close is the one that would fold the column — nothing
      // left behind, the list not open — the list comes back instead. Any other
      // close keeps the previous rule, so the list never lands over a tab that
      // is still open.
      if (!current.current.showFiles && current.current.surfaces.every((s) => closed.has(keyOf(s)))) {
        if (fromExplorer?.current && closed.has(`file:${fromExplorer.current}`)) {
          fromExplorer.current = "";
          setLauncher?.(false);
          setShowFiles?.(true);
        } else {
          onCloseManual();
        }
      }
    } finally { pending.current = false; setClosing(false); }
  };
  return { close, closing, closeFailure };
}
