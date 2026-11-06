import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import type { MemoryProactive } from "../port/memory";

const MODES: [MemoryProactive, string][] = [["off", "memory.proactive_off"], ["ask", "memory.proactive_ask"], ["auto", "memory.proactive_auto"]];

// One line at the top of the memory page: what the model does when it sees
// something worth keeping for good.
export function MemoryProactiveRow({ port }: { port: AgentPort }) {
  const [mode, setMode] = useState<MemoryProactive | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    let live = true;
    port.memoryProactive().then((m) => { if (live) setMode(m); }).catch((e) => { if (live) setError(reason(e)); });
    return () => { live = false; };
  }, [port]);
  const pick = (next: MemoryProactive) => {
    const was = mode;
    setMode(next);
    setError("");
    port.setMemoryProactive(next).catch((e) => { setMode(was); setError(reason(e)); });
  };
  if (mode === null && !error) return null;
  return (
    <div className="memproactive">
      <div>
        <b>{t("memory.proactive_title")}</b>
        <span>{t("memory.proactive_hint")}</span>
        {error && <span role="alert" className="memproactive-err">{error}</span>}
      </div>
      <div className="memproactive-seg" role="radiogroup" aria-label={t("memory.proactive_title")}>
        {MODES.map(([id, label]) => (
          <button key={id} type="button" role="radio" aria-checked={mode === id} data-on={mode === id ? "" : undefined} data-action="memory.proactive" data-target={id} onClick={() => pick(id)}>{t(label)}</button>
        ))}
      </div>
    </div>
  );
}
