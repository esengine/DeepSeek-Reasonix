export type TerminalOutputSink = {
  sessionID: string;
  write: (data: string) => void;
};

export function createTerminalOutputRouter() {
  const pending = new Map<string, string>();

  const write = (sessionID: string, data: string, sinks: TerminalOutputSink[]) => {
    const match = sinks.find((sink) => sink.sessionID === sessionID);
    if (match) {
      match.write(data);
      return;
    }
    pending.set(sessionID, (pending.get(sessionID) ?? "") + data);
  };

  const takePending = (sessionID: string): string => {
    const buffered = pending.get(sessionID) ?? "";
    pending.delete(sessionID);
    return buffered;
  };

  const clearPending = (sessionID: string) => {
    pending.delete(sessionID);
  };

  return { write, takePending, clearPending };
}
