import type { ComposerInvocation } from "./invocationDisplay";
import type { SessionReference } from "./types";
import type { SelectedTextReference } from "./selectedTextContext";
import type { Attachment } from "./composerAttachments";

export interface WorkspaceReference {
  path: string;
  isDir?: boolean;
  displayPath?: string;
}

export type PastedBlock = { label: string; text: string };

export type PersistentComposerDraft = {
  text: string;
  invocations: ComposerInvocation[];
  attachments: Attachment[];
  workspaceRefs: WorkspaceReference[];
  pastedBlocks: PastedBlock[];
  openPastedLabels: string[];
  sessionRefs: SessionReference[];
  selectedTextRefs: SelectedTextReference[];
};

export type PersistentComposerTarget = {
  draftId: string;
  generation: number;
  initial: PersistentComposerDraft;
  revision: number;
  onChange: (draftId: string, generation: number, content: PersistentComposerDraft) => void;
  onPatch?: (draftId: string, generation: number, patch: Partial<PersistentComposerDraft> | ((content: PersistentComposerDraft) => PersistentComposerDraft)) => void;
  isCurrent?: (draftId: string, generation: number) => boolean;
  canEdit?: (draftId: string, generation: number) => boolean;
  onTaskError?: (draftId: string, generation: number, message: string) => void;
  trackTask?: <T>(draftId: string, generation: number, promise: Promise<T>) => Promise<T>;
};
