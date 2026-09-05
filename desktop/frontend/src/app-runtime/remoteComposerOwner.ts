import type { SessionOperationAuthority } from "./useResourceOperations";
import type { RemoteSessionApi } from "../lib/useRemoteSession";
import type { RemoteTabRefView } from "../lib/types";
import type { RemoteNavigationCommand } from "../lib/remoteNavigationCommands";
import { CommandCancelled } from "../lib/commandOutcome";

type RemoteSendPorts = Pick<RemoteSessionApi, "compact" | "runManagementCommand" | "setModel" | "setEffort"> & {
  send: (display: string, submit: string) => Promise<void>;
  applyGoal: (tab: string, goal: string) => Promise<unknown>;
  requestClear: () => void;
  newSession: RemoteNavigationCommand;
};
export type RemoteSendInput = {
  tabId: string;
  remote?: RemoteTabRefView;
  activateGoal: boolean;
  display: string;
  submit: string;
  commandText: string;
  command: ReturnType<typeof import("../lib/useRemoteComposerIntegration").remoteRuntimeCommand>;
  ports: RemoteSendPorts;
};

export async function executeRemoteSend(input: RemoteSendInput, authority: SessionOperationAuthority): Promise<void> {
  const { command, ports } = input;
  authority.checkpoint();
  if (command?.method === "clearSession") { if (authority.ownsUI()) ports.requestClear(); return; }
  if (command?.method === "newSession") {
    if (input.remote && authority.ownsUI()) {
      const outcome = await ports.newSession(input.remote, { newSession: true });
      if (outcome.status === "failed") throw outcome.error;
      if (outcome.status === "cancelled") throw new CommandCancelled(outcome.reason);
    }
    return;
  }
  if (command?.method === "compact") return ports.compact(command.value);
  if (command?.method === "runManagementCommand") return ports.runManagementCommand(input.commandText, command.rehydrate);
  if (command?.method === "setModel" || command?.method === "setEffort") return ports[command.method](command.value);
  if (input.activateGoal) {
    await ports.applyGoal(input.tabId, input.commandText);
    authority.checkpoint();
  }
  await ports.send(input.display, input.submit);
}

export type ComposerRuntimeInput = {
  tabId: string;
  remote: boolean;
  action: "pause" | "resume" | "effort";
  level?: string;
  ports: Pick<RemoteSessionApi, "pauseGoal" | "resumeGoal" | "setEffort"> & {
    pauseLocal: (tab: string) => Promise<unknown>;
    resumeLocal: (tab: string) => Promise<unknown>;
    effortLocal: (tab: string, level: string) => Promise<void>;
  };
};
export async function executeComposerRuntime(input: ComposerRuntimeInput, authority: SessionOperationAuthority) {
  authority.checkpoint();
  const { ports, tabId, remote } = input;
  if (input.action === "pause") await (remote ? ports.pauseGoal() : ports.pauseLocal(tabId));
  else if (input.action === "resume") await (remote ? ports.resumeGoal() : ports.resumeLocal(tabId));
  else await (remote ? ports.setEffort(input.level ?? "") : ports.effortLocal(tabId, input.level ?? ""));
}
