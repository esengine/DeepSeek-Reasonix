import { num, record, str, strList, type Params } from "./params.js";
import { RpcError } from "./rpc.js";

export interface WindowHostApi {
  show(reason: string): void;
  hide(): void;
  maximise(): void;
  unmaximise(): void;
  minimise(): void;
  unminimise(): void;
  toggleMaximise(): void;
  center(): void;
  isMaximised(): boolean;
  isMinimised(): boolean;
  setPosition(x: number, y: number): void;
  setTitle(title: string): void;
  toggleDevTools(): void;
}

export interface DialogHostApi {
  openDirectory(params: Params): Promise<{ path: string }>;
  openFile(params: Params): Promise<{ paths: string[] }>;
  saveFile(params: Params): Promise<{ path: string }>;
  message(params: Params): Promise<{ button: string }>;
}

export interface TrayLabels {
  openTitle: string;
  openTooltip: string;
  quitTitle: string;
  quitTooltip: string;
  tooltip: string;
}

export interface TrayHostApi {
  ensure(labels: TrayLabels): { ready: boolean; reason: string };
  destroy(): void;
}

export interface RemoteWindowHostApi {
  open(input: { hostKey: string; url: string; title: string }): { windowId: string };
  navigate(input: { hostKey: string; url: string; title: string }): void;
  focus(hostKey: string): void;
  close(hostKey: string): void;
}

export interface LifecycleHostApi {
  approve(): void;
  relaunch(args: string[], execPath?: string): void;
}

export interface ScreenInfo {
  x: number;
  y: number;
  width: number;
  height: number;
  scale: number;
  primary: boolean;
}

export type HostCall = (params: Params) => Promise<unknown> | unknown;
export type HostCallTable = Record<string, HostCall>;

export interface HostCallDeps {
  window: WindowHostApi;
  dialogs: DialogHostApi;
  tray: TrayHostApi;
  remote: RemoteWindowHostApi;
  lifecycle: LifecycleHostApi;
  openExternal(url: string): Promise<void>;
  hideApp(): void;
  screens(): ScreenInfo[];
  browser?: HostCallTable;
}

const remoteInput = (params: Params) => ({ hostKey: str(params, "hostKey"), url: str(params, "url"), title: str(params, "title") });
const EXTERNAL_PROTOCOLS = new Set(["http:", "https:", "mailto:"]);

function externalURL(value: string): string {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new Error("refusing to open an invalid external URL");
  }
  if (!EXTERNAL_PROTOCOLS.has(url.protocol)) throw new Error(`refusing to open external URL with protocol ${url.protocol}`);
  return url.href;
}

export function buildHostCallTable(deps: HostCallDeps): HostCallTable {
  const done = (run: () => void): HostCall => (params) => {
    void params;
    run();
    return {};
  };
  return {
    "host/window.show": (params) => {
      deps.window.show(str(params, "reason"));
      return {};
    },
    "host/window.hide": done(() => deps.window.hide()),
    "host/app.hide": done(() => deps.hideApp()),
    "host/window.maximise": done(() => deps.window.maximise()),
    "host/window.unmaximise": done(() => deps.window.unmaximise()),
    "host/window.minimise": done(() => deps.window.minimise()),
    "host/window.unminimise": done(() => deps.window.unminimise()),
    "host/window.toggleMaximise": done(() => deps.window.toggleMaximise()),
    "host/window.center": done(() => deps.window.center()),
    "host/window.isMaximised": () => ({ value: deps.window.isMaximised() }),
    "host/window.isMinimised": () => ({ value: deps.window.isMinimised() }),
    "host/window.setPosition": (params) => {
      deps.window.setPosition(num(params, "x"), num(params, "y"));
      return {};
    },
    "host/window.setTitle": (params) => {
      deps.window.setTitle(str(params, "title"));
      return {};
    },
    "host/screen.list": () => ({ screens: deps.screens() }),
    "host/dialog.openDirectory": (params) => deps.dialogs.openDirectory(params),
    "host/dialog.openFile": (params) => deps.dialogs.openFile(params),
    "host/dialog.saveFile": (params) => deps.dialogs.saveFile(params),
    "host/dialog.message": (params) => deps.dialogs.message(params),
    "host/shell.openExternal": async (params) => {
      await deps.openExternal(externalURL(str(params, "url")));
      return {};
    },
    "host/app.quit": done(() => deps.lifecycle.approve()),
    "host/app.relaunch": (params) => {
      const execPath = str(params, "execPath");
      if (execPath) deps.lifecycle.relaunch(strList(params, "args"), execPath);
      else deps.lifecycle.relaunch(strList(params, "args"));
      return {};
    },
    "host/devtools.toggle": done(() => deps.window.toggleDevTools()),
    "host/remoteWindow.open": (params) => deps.remote.open(remoteInput(params)),
    "host/remoteWindow.navigate": (params) => {
      deps.remote.navigate(remoteInput(params));
      return {};
    },
    "host/remoteWindow.focus": (params) => {
      deps.remote.focus(str(params, "hostKey"));
      return {};
    },
    "host/remoteWindow.close": (params) => {
      deps.remote.close(str(params, "hostKey"));
      return {};
    },
    "host/tray.ensure": (params) => deps.tray.ensure({
      openTitle: str(params, "openTitle", "Open"),
      openTooltip: str(params, "openTooltip"),
      quitTitle: str(params, "quitTitle", "Quit"),
      quitTooltip: str(params, "quitTooltip"),
      tooltip: str(params, "tooltip", "Reasonix"),
    }),
    "host/tray.destroy": done(() => deps.tray.destroy()),
    ...(deps.browser ?? {}),
  };
}

export async function dispatchHostCall(table: HostCallTable, method: string, params: unknown): Promise<unknown> {
  const call = Object.prototype.hasOwnProperty.call(table, method) ? table[method] : undefined;
  if (!call) throw new RpcError(-32601, `unknown host method: ${method}`);
  return call(record(params));
}
