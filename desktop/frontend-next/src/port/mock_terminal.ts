import { HttpError } from "./http_error";
import { MockToolOutput } from "./mock_tooloutput";
import type { ShellChoice, TerminalFrame, TerminalInfo, TerminalStream } from "./terminal";

const SHELLS: ShellChoice[] = [
  { name: "Git Bash", kind: "bash", path: "/usr/bin/bash" },
  { name: "PowerShell", kind: "pwsh", path: "/usr/bin/pwsh" },
  { name: "命令提示符", kind: "cmd", path: "cmd.exe" },
];

const PROMPT = "\x1b[36m~/work/reasonix\x1b[0m \x1b[32m(main)\x1b[0m $ ";

interface Fake {
  info: TerminalInfo;
  held: string;
  line: string;
  readers: Set<(f: TerminalFrame) => void>;
}

const encode = (text: string) => btoa(String.fromCharCode(...new TextEncoder().encode(text)));

// A shell that echoes what is typed and answers a few commands, enough to draw
// every state of the terminal page without a kernel.
export class MockTerminal extends MockToolOutput {
  private fakes = new Map<string, Fake>();
  private serial = 0;

  private say(fake: Fake, text: string) {
    fake.held += text;
    for (const reader of fake.readers) reader({ kind: "out", data: encode(text) });
  }

  async terminalShells() {
    return SHELLS;
  }

  async terminals() {
    return [...this.fakes.values()].map((f) => ({ ...f.info }));
  }

  async openTerminal(shell: string, cols: number, rows: number) {
    const choice = SHELLS.find((s) => !shell || s.kind === shell);
    if (!choice) throw new HttpError(400, "that shell is not on this machine", { code: "terminal.shell_missing" });
    const info: TerminalInfo = { id: `t${(this.serial += 1)}`, name: choice.name, shell: choice.path, dir: "~/work/reasonix", cols, rows, running: true, busy: false, created: new Date().toISOString() };
    const fake: Fake = { info, held: PROMPT, line: "", readers: new Set() };
    this.fakes.set(info.id, fake);
    return { ...info };
  }

  private fake(id: string) {
    const fake = this.fakes.get(id);
    if (!fake) throw new HttpError(404, "no such terminal", { code: "terminal.not_found" });
    return fake;
  }

  async closeTerminal(id: string) {
    this.fake(id);
    this.fakes.delete(id);
  }

  async restartTerminal(id: string) {
    const fake = this.fake(id);
    if (!fake.info.running) {
      fake.info = { ...fake.info, running: true, exitCode: undefined };
      fake.held = PROMPT;
    }
    return { ...fake.info };
  }

  async resizeTerminal(id: string, cols: number, rows: number) {
    const fake = this.fake(id);
    fake.info = { ...fake.info, cols, rows };
  }

  async terminalInput(id: string, data: string) {
    const fake = this.fake(id);
    if (!fake.info.running) throw new HttpError(409, "the shell has ended", { code: "terminal.exited" });
    for (const ch of data) {
      if (ch === "\r") {
        const command = fake.line.trim();
        fake.line = "";
        this.say(fake, "\r\n");
        if (command === "exit") {
          fake.info = { ...fake.info, running: false, exitCode: 130 };
          for (const reader of fake.readers) reader({ kind: "exit", code: 130 });
          return;
        }
        if (command) this.say(fake, command.startsWith("echo ") ? command.slice(5) + "\r\n" : `${command}: command not found\r\n`);
        this.say(fake, PROMPT);
      } else if (ch === "\x7f") {
        if (fake.line) {
          fake.line = fake.line.slice(0, -1);
          this.say(fake, "\b \b");
        }
      } else if (ch >= " ") {
        fake.line += ch;
        this.say(fake, ch);
      }
    }
  }

  streamTerminal(id: string, onFrame: (frame: TerminalFrame) => void, onEnd: () => void): TerminalStream {
    const fake = this.fakes.get(id);
    if (!fake) {
      queueMicrotask(onEnd);
      return { close() {} };
    }
    queueMicrotask(() => {
      onFrame({ kind: "replay", data: encode(fake.held) });
      if (!fake.info.running) onFrame({ kind: "exit", code: fake.info.exitCode });
    });
    fake.readers.add(onFrame);
    return { close: () => void fake.readers.delete(onFrame) };
  }
}
