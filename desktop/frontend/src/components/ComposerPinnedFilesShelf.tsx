import { Suspense, lazy } from "react";
import type { PinnedFileInfo } from "../lib/pinnedContextBridge";

const PinnedFilesShelf = lazy(() => import("./PinnedFilesShelf").then((module) => ({ default: module.PinnedFilesShelf })));

export function ComposerPinnedFilesShelf({ tabId, pinnedFiles }: { tabId: string; pinnedFiles?: PinnedFileInfo[] }) {
  if (!pinnedFiles?.length) return null;
  return <Suspense fallback={null}><PinnedFilesShelf tabId={tabId} pinnedFiles={pinnedFiles} /></Suspense>;
}
