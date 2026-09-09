export interface PinnedFileInfo {
  path: string;
  sizeBytes: number;
  tokenEstimate: number;
  error?: string;
}

export interface PinnedContextBindings {
  PinFileForTab(tabID: string, rel: string): Promise<PinnedFileInfo>;
  UnpinFileForTab(tabID: string, rel: string): Promise<void>;
  GetPinnedFilesForTab(tabID: string): Promise<PinnedFileInfo[]>;
}

export function makeMockPinnedContextBindings(): PinnedContextBindings {
  return {
    async PinFileForTab(_tabID: string, rel: string) {
      return { path: rel, sizeBytes: 1024, tokenEstimate: 256 };
    },
    async UnpinFileForTab(_tabID: string, _rel: string) {},
    async GetPinnedFilesForTab(_tabID: string) { return []; },
  };
}
