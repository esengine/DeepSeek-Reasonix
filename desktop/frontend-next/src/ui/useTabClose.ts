import { useRef, useState, type Dispatch, type SetStateAction } from "react";
import type { AgentPort } from "../port/port";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { keyOf, labelOf, type Surface } from "./workbench_tabs";

type Setter<T> = Dispatch<SetStateAction<T>>;
interface Props {
  port: AgentPort; surfaces: Surface[]; selected: string; showFiles: boolean;
  setBrowsers: Setter<string[]>; setOpenFiles: Setter<string[]>; setDismissed: Setter<Set<string>>;
  setSelected: Setter<string>; onCloseManual: () => void; onCloseSelected: () => void;
}

export function useTabClose({ port, surfaces, selected, showFiles, setBrowsers, setOpenFiles, setDismissed, setSelected, onCloseManual, onCloseSelected }: Props) {
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
      setDismissed((v) => new Set([...v, ...closed]));
      if (closed.has(current.current.selected)) onCloseSelected();
      setSelected((v) => closed.has(v) ? "" : v);
      setCloseFailure(failures.join("\n"));
      if (!current.current.showFiles && current.current.surfaces.every((s) => closed.has(keyOf(s)))) onCloseManual();
    } finally { pending.current = false; setClosing(false); }
  };
  return { close, closing, closeFailure };
}
