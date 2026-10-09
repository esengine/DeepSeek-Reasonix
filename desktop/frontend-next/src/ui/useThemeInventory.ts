import { useCallback, useEffect, useRef, useState } from "react";
import type { AgentPort, ThemePack } from "../port/port";
import { reason } from "../i18n/kernel";

export function useThemeInventory(port: AgentPort) {
  const [connection, setConnection] = useState({ port });
  const current = useRef<typeof connection | null>(connection);
  current.current = connection;
  const requests = useRef(0);
  const [themes, setThemes] = useState<{ packs: ThemePack[]; unread: string }>({ packs: [], unread: "" });
  if (connection.port !== port) {
    setConnection({ port });
    setThemes({ packs: [], unread: "" });
  }

  const load = useCallback(() => {
    if (current.current !== connection) return;
    const request = ++requests.current;
    port.themes()
      .then((packs) => {
        if (current.current === connection && requests.current === request) setThemes({ packs, unread: "" });
      })
      .catch((e) => {
        if (current.current === connection && requests.current === request) setThemes((prev) => ({ ...prev, unread: reason(e) }));
      });
  }, [port, connection]);

  useEffect(() => {
    current.current = connection;
    load();
    return () => { if (current.current === connection) current.current = null; };
  }, [connection, load]);

  return { ...themes, load };
}
