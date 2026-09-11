import { useEffect, useState } from "react";
import { asArray } from "./array";
import { app } from "./bridge";
import type { CommandInfo } from "./types";

export function useComposerCommandCatalog(
  supplied: readonly CommandInfo[] | undefined,
  ready: boolean,
  cwd: string | undefined,
  running: boolean,
  workspaceScopeKey: string,
): CommandInfo[] {
  const [commands, setCommands] = useState<CommandInfo[]>([]);
  useEffect(() => {
    if (supplied) {
      setCommands([...supplied]);
      return;
    }
    let live = true;
    app.Commands()
      .then((next) => { if (live) setCommands(asArray(next)); })
      .catch(() => {});
    return () => { live = false; };
  }, [cwd, ready, running, supplied, workspaceScopeKey]);
  return commands;
}
