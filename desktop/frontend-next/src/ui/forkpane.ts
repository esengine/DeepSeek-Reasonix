import { useCallback, type Dispatch, type SetStateAction } from "react";
import { t } from "../i18n";
import type { Checkpoint } from "../port/port";
import type { HubPort, RuntimeView } from "../port/hub";

interface Inputs {
  hub: HubPort;
  runtimes: RuntimeView[];
  setRuntimes: Dispatch<SetStateAction<RuntimeView[]>>;
  focusPane: (id: string) => void;
  reloadPanes: () => Promise<void>;
}

export function useForkPane({ hub, runtimes, setRuntimes, focusPane, reloadPanes }: Inputs) {
  return useCallback(async (id: string, checkpoint: Checkpoint) => {
    const source = runtimes.find((rt) => rt.id === id);
    if (!source?.sessionPath || checkpoint.msgIndex === undefined || !checkpoint.stamp) {
      throw new Error(t("这轮对话已发生变化，请刷新后重试"));
    }
    const view = await hub.fork(id, {
      sessionPath: source.sessionPath, turn: checkpoint.turn,
      msgIndex: checkpoint.msgIndex, stamp: checkpoint.stamp,
    });
    if (!view.sessionPath) throw new Error(t("分支已创建，但暂时无法打开，请重试。"));
    setRuntimes((prev) => [...prev.filter((rt) => rt.id !== view.id), view]);
    focusPane(view.id);
    await reloadPanes();
  }, [hub, runtimes, setRuntimes, focusPane, reloadPanes]);
}
