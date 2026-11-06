import { SseWorkspace } from "./sse_workspace";
import type { ShellChoice, TerminalFrame, TerminalInfo, TerminalStream } from "./terminal";

export class SseTerminal extends SseWorkspace {
  terminalShells() {
    return this.get<ShellChoice[]>("/terminals/shells");
  }
  terminals() {
    return this.get<TerminalInfo[]>("/terminals");
  }
  openTerminal(shell: string, cols: number, rows: number) {
    return this.post0<TerminalInfo>("/terminals", { shell, cols, rows });
  }
  closeTerminal(id: string) {
    return this.del("/terminals/" + encodeURIComponent(id));
  }
  restartTerminal(id: string) {
    return this.post0<TerminalInfo>("/terminals/" + encodeURIComponent(id) + "/restart");
  }
  resizeTerminal(id: string, cols: number, rows: number) {
    return this.post("/terminals/" + encodeURIComponent(id) + "/resize", { cols, rows });
  }
  terminalInput(id: string, data: string) {
    return this.post("/terminals/" + encodeURIComponent(id) + "/input", { data });
  }
  // A stream that ends does not reconnect by itself: the exit frame is the
  // normal end, and a dropped connection is for the view to reopen with replay.
  streamTerminal(id: string, onFrame: (frame: TerminalFrame) => void, onEnd: () => void): TerminalStream {
    const source = new EventSource(`${this.base}/terminals/${encodeURIComponent(id)}/stream`, { withCredentials: true });
    source.onmessage = (e) => {
      try {
        onFrame(JSON.parse(e.data) as TerminalFrame);
      } catch {
        // A frame that is not ours carries nothing to draw.
      }
    };
    source.onerror = () => {
      source.close();
      onEnd();
    };
    return { close: () => source.close() };
  }
}
