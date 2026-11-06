import type { AgentPort } from "../port/port";
import type { ShellChoice, TerminalInfo } from "../port/terminal";
import { TerminalView } from "./TerminalView";

export interface TerminalLayerProps {
  port: AgentPort;
  terminals: TerminalInfo[];
  active: string;
  shown: boolean;
  scheme: "light" | "dark";
  shells: ShellChoice[];
  onLoadShells: () => void;
  onShell: (kind: string) => void;
  onQuote: (text: string) => void;
  onOpenPage: (url: string) => void;
  onCommand: (id: string, line: string) => void;
  onRestart: (id: string) => Promise<void>;
  onClose: (id: string) => void;
}

// Every terminal stays mounted, whichever tab is in front, so a shell's screen
// is where it was left; only the one in front is read from the kernel.
export default function TerminalLayer({ port, terminals, active, shown, scheme, shells, onLoadShells, onShell, onQuote, onOpenPage, onCommand, onRestart, onClose }: TerminalLayerProps) {
  return (
    <>
      {terminals.map((info) => (
        <TerminalView key={info.id} port={port} info={info} shown={shown && info.id === active} scheme={scheme} shells={shells}
          onLoadShells={onLoadShells} onShell={onShell} onQuote={onQuote} onOpenPage={onOpenPage}
          onCommand={(line) => onCommand(info.id, line)} onRestart={() => onRestart(info.id)} onClose={() => onClose(info.id)} />
      ))}
    </>
  );
}
