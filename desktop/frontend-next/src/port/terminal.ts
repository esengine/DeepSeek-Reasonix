// The person's own terminals: shells they type into, apart from the model's.

export interface TerminalInfo {
  id: string;
  name: string;
  shell: string;
  dir: string;
  cols: number;
  rows: number;
  running: boolean;
  exitCode?: number;
  busy: boolean;
  created: string;
}

// An interpreter this machine has. kind is the short tag beside the name and is
// what openTerminal takes to pick it.
export interface ShellChoice {
  name: string;
  kind: string;
  path: string;
  args?: string[];
}

// What a terminal's stream carries. replay is what the shell wrote before this
// reader came; out is what it writes after; exit ends the stream with the code.
export interface TerminalFrame {
  kind: "replay" | "out" | "exit";
  data?: string;
  code?: number;
}

export interface TerminalStream {
  close(): void;
}

export interface TerminalPort {
  terminalShells(): Promise<ShellChoice[]>;
  terminals(): Promise<TerminalInfo[]>;
  openTerminal(shell: string, cols: number, rows: number): Promise<TerminalInfo>;
  closeTerminal(id: string): Promise<void>;
  restartTerminal(id: string): Promise<TerminalInfo>;
  resizeTerminal(id: string, cols: number, rows: number): Promise<void>;
  // The keystrokes as typed. A refusal says the shell has ended; the exit footer
  // is already showing then, so a caller may let it go.
  terminalInput(id: string, data: string): Promise<void>;
  // Reads the terminal from what it already held. onEnd fires when the stream
  // stops for any reason, so a caller knows to reopen it rather than wait.
  streamTerminal(id: string, onFrame: (frame: TerminalFrame) => void, onEnd: () => void): TerminalStream;
}
