import type { BrowserWindow, Dialog, MessageBoxOptions, OpenDialogOptions, SaveDialogOptions } from "electron";
import { join } from "node:path";
import { bool, str, strList, type Params } from "./params.js";

export interface DialogFilter {
  name: string;
  extensions: string[];
}

// Wails filters carry `*.png;*.jpg` patterns; Electron wants bare extensions.
export function fileFiltersFromPatterns(value: unknown): DialogFilter[] {
  if (!Array.isArray(value)) return [];
  const filters: DialogFilter[] = [];
  for (const entry of value) {
    if (typeof entry !== "object" || entry === null) continue;
    const { displayName, pattern } = entry as { displayName?: unknown; pattern?: unknown };
    if (typeof pattern !== "string") continue;
    const extensions = pattern
      .split(";")
      .map((part) => part.trim())
      .filter((part) => part !== "")
      .map((part) => (part === "*" || part === "*.*" ? "*" : part.replace(/^\*?\./, "")))
      .filter((part) => part !== "");
    if (extensions.length === 0) continue;
    filters.push({ name: typeof displayName === "string" && displayName !== "" ? displayName : pattern, extensions });
  }
  return filters;
}

export type DialogApi = Pick<Dialog, "showOpenDialog" | "showSaveDialog" | "showMessageBox">;

type MessageType = "info" | "warning" | "error" | "question";

function messageType(value: string): MessageType {
  return value === "warning" || value === "error" || value === "question" ? value : "info";
}

export class DialogHost {
  constructor(private readonly dialog: DialogApi, private readonly parent: () => BrowserWindow | undefined) {}

  private open(options: OpenDialogOptions) {
    const parent = this.parent();
    return parent ? this.dialog.showOpenDialog(parent, options) : this.dialog.showOpenDialog(options);
  }

  private save(options: SaveDialogOptions) {
    const parent = this.parent();
    return parent ? this.dialog.showSaveDialog(parent, options) : this.dialog.showSaveDialog(options);
  }

  private box(options: MessageBoxOptions) {
    const parent = this.parent();
    return parent ? this.dialog.showMessageBox(parent, options) : this.dialog.showMessageBox(options);
  }

  async openDirectory(params: Params): Promise<{ path: string }> {
    const result = await this.open({
      title: str(params, "title") || undefined,
      defaultPath: str(params, "defaultDirectory") || undefined,
      properties: ["openDirectory", "createDirectory"],
    });
    return { path: result.canceled ? "" : (result.filePaths[0] ?? "") };
  }

  async openFile(params: Params): Promise<{ paths: string[] }> {
    const result = await this.open({
      title: str(params, "title") || undefined,
      defaultPath: str(params, "defaultDirectory") || undefined,
      filters: fileFiltersFromPatterns(params.filters),
      properties: bool(params, "multiple") ? ["openFile", "multiSelections"] : ["openFile"],
    });
    return { paths: result.canceled ? [] : result.filePaths };
  }

  async saveFile(params: Params): Promise<{ path: string }> {
    const directory = str(params, "defaultDirectory");
    const filename = str(params, "defaultFilename");
    const defaultPath = directory && filename ? join(directory, filename) : directory || filename || undefined;
    const result = await this.save({
      title: str(params, "title") || undefined,
      defaultPath,
      filters: fileFiltersFromPatterns(params.filters),
    });
    return { path: result.canceled ? "" : (result.filePath ?? "") };
  }

  async message(params: Params): Promise<{ button: string }> {
    const buttons = strList(params, "buttons");
    const labels = buttons.length > 0 ? buttons : ["OK"];
    const defaultIndex = labels.indexOf(str(params, "defaultButton"));
    const cancelIndex = labels.indexOf(str(params, "cancelButton"));
    const result = await this.box({
      type: messageType(str(params, "type")),
      title: str(params, "title"),
      message: str(params, "message"),
      buttons: labels,
      defaultId: defaultIndex >= 0 ? defaultIndex : 0,
      cancelId: cancelIndex >= 0 ? cancelIndex : -1,
      noLink: true,
    });
    return { button: labels[result.response] ?? "" };
  }
}
