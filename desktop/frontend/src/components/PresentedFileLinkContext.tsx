import { createContext, useContext, useMemo, type ReactNode } from "react";
import type { PresentedFileView } from "../lib/chatViewSource";
import { openResource, type FileResourceRef } from "../lib/presentedFileNavigation";

type PresentedFileLink = { path: string; open: () => void };
const PresentedFileLinkContext = createContext<ReadonlyMap<string, PresentedFileLink>>(new Map());

function basename(path: string): string {
  return path.replaceAll("\\", "/").split("/").filter(Boolean).pop() ?? path;
}

export function PresentedFileLinkProvider({ files, tabId, hostId, children }: { files: readonly PresentedFileView[]; tabId?: string; hostId?: string; children: ReactNode }) {
  const links = useMemo(() => {
    const next = new Map<string, PresentedFileLink>();
    const basenameCounts = new Map<string, number>();
    for (const file of files) basenameCounts.set(basename(file.path), (basenameCounts.get(basename(file.path)) ?? 0) + 1);
    for (const file of files) {
      const ref: FileResourceRef = { source: "presented", hostId: hostId ?? "local", tabId: tabId ?? "", toolCallId: file.toolCallId, path: file.path };
      const link = { path: file.path, open: () => { void openResource(ref, { view: "preview" }); } };
      next.set(file.path, link);
      if (basenameCounts.get(basename(file.path)) === 1) next.set(basename(file.path), link);
    }
    return next;
  }, [files, hostId, tabId]);
  return <PresentedFileLinkContext.Provider value={links}>{children}</PresentedFileLinkContext.Provider>;
}

export function usePresentedFileLink(text: string): PresentedFileLink | undefined {
  return useContext(PresentedFileLinkContext).get(text.trim());
}
