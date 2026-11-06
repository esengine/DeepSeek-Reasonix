import { lazy, Suspense } from "react";
import { t } from "../i18n";
import type { AgentPort } from "../port/port";
import { TerminalCloseAsk } from "./TerminalCloseAsk";
import type { useTerminalTabs } from "./useTerminalTabs";
import type { Surface } from "./workbench_tabs";

const TerminalLayer = lazy(() => import("./TerminalLayer"));

// The screens are loaded with the first terminal, not with the workbench: the
// emulator is most of what a terminal costs.
export function WorkbenchTerminals({ port, hook, active, shown, scheme, onQuote, onClose }: {
  port: AgentPort;
  hook: ReturnType<typeof useTerminalTabs>;
  active: string;
  shown: boolean;
  scheme: "light" | "dark";
  onQuote?: (text: string, source: string) => void;
  onClose: (targets: Surface[]) => void;
}) {
  const named = (id: string) => hook.tabs.find((tab) => tab.id === id);
  return (
    <>
      {hook.terminals.length > 0 && (
        <Suspense fallback={null}>
          <TerminalLayer port={port} terminals={hook.terminals} active={active} shown={shown} scheme={scheme} shells={hook.shells}
            onLoadShells={hook.loadShells} onShell={(kind) => void hook.open(kind)} onQuote={(text) => onQuote?.(text, t("terminal.name"))}
            onOpenPage={(url) => void hook.openUrl(url)} onCommand={hook.submitted} onRestart={hook.restart}
            onClose={(id) => { const tab = named(id); if (tab) onClose([tab]); }} />
        </Suspense>
      )}
      {hook.asking && <TerminalCloseAsk anchor={hook.asking.anchor} onCancel={hook.cancelAsk} onConfirm={hook.confirmAsk} />}
    </>
  );
}
