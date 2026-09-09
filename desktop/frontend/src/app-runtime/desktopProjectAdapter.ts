import { app } from "../lib/bridge";

export const desktopProjectAdapter = {
  renameLocal: (id: string, title: string) => app.RenameTopic(id, title),
  listRemote: (host: string, workspace: string) => app.RemoteProjectSessions(host, workspace),
  renameRemote: (host: string, workspace: string, name: string, title: string) => app.RenameRemoteProjectSession(host, workspace, name, title),
};
