import type { AppBindings } from "./bridge";
import type { ComposerTarget } from "../generated/desktopContract.generated";

export interface AttachmentBindings {
  StageImageForTab(tabID: string, operationID: string, displayName: string, mime: string, dataUrl: string): Promise<{ draftId: string; displayName: string; mime: string; width: number; height: number; bytes: number }>;
  ReadDraftImageForTab(tabID: string, draftID: string): Promise<string>;
  ReleaseDraftImageForTab(tabID: string, draftID: string): Promise<void>;
  SavePastedImageForTab(tabID: string, dataURL: string): Promise<string>;
  SavePastedFileForTab(tabID: string, name: string, dataURL: string): Promise<string>;
  SaveClipboardImageForTab(tabID: string): Promise<string>;
  AttachmentDataURLForTab(tabID: string, path: string): Promise<string>;
  AttachDroppedForTab(tabID: string, path: string): Promise<{ kind: string; path: string; isDir?: boolean; displayPath?: string; previewUrl?: string }>;
  ReadSessionAttachmentForTab(tabID: string, digest: string, offset: number): Promise<{ data: string; nextOffset: number; done: boolean }>;
  StartTurnForTabWithDrafts(tabID: string, input: string, submissionID: string, draftIDs: string[]): Promise<{ turnId: string; status: string; submissionId: string }>;
  CaptureAttachmentTarget?(target: ComposerTarget): Promise<{ token: string; capabilities: string[] }>;
  ReleaseAttachmentTarget?(token: string): Promise<void>;
  SavePastedFileForTarget?(token: string, name: string, dataURL: string): Promise<string>;
  SaveClipboardImageForTarget?(token: string): Promise<string>;
  AttachmentDataURLForTarget?(token: string, path: string): Promise<string>;
  AttachDroppedForTarget?(token: string, path: string): Promise<{ kind: string; path: string; isDir?: boolean; displayPath?: string; previewUrl?: string }>;
  StageImageForTarget?(token: string, operationID: string, displayName: string, mime: string, dataUrl: string): Promise<{ draftId: string; path?: string; displayName: string; mime: string; width: number; height: number; bytes: number }>;
  ReadDraftImageForTarget?(token: string, draftID: string): Promise<string>;
  RebindDraftImageForTarget?(token: string, draftID: string): Promise<{ draftId: string; displayName: string; mime: string; width: number; height: number; bytes: number }>;
  ReleaseDraftImageForTarget?(token: string, draftID: string): Promise<void>;
  StartTurnForAttachmentTarget?(token: string, submissionID: string, request: { input: string; display: string; original?: string; goal?: string; toolApprovalMode?: string; invocations?: import("./invocationDisplay").InvocationRequest[]; attachments: import("./invocationDisplay").SubmissionAttachment[] }): Promise<{ turnId: string; status: string; submissionId: string }>;
  EnqueueForAttachmentTarget?(token: string, submissionID: string, input: string, display: string, invocations: import("./invocationDisplay").InvocationRequest[], attachments: import("./invocationDisplay").SubmissionAttachment[]): Promise<{ itemId: string; paused?: boolean; error?: string }>;
}

const mockImages = new Map<string, string>();
const mockPreviewImageDataURL =
  "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='160' height='120' viewBox='0 0 160 120'%3E%3Cdefs%3E%3ClinearGradient id='g' x1='0' y1='0' x2='1' y2='1'%3E%3Cstop offset='0' stop-color='%23f97316'/%3E%3Cstop offset='1' stop-color='%232563eb'/%3E%3C/linearGradient%3E%3C/defs%3E%3Crect width='160' height='120' rx='14' fill='url(%23g)'/%3E%3Ccircle cx='44' cy='38' r='16' fill='%23fff7ed' opacity='.9'/%3E%3Cpath d='M18 96 62 58l24 22 18-16 38 32z' fill='%23ffffff' opacity='.9'/%3E%3C/svg%3E";

export function makeMockAttachmentBindings(): AttachmentBindings & Partial<AppBindings> & ThisType<AppBindings> {
  return {
    async StageImageForTab(_tabID: string, _operation: string, displayName: string, mime: string, dataURL: string) {
      const draftId = crypto.randomUUID().replaceAll("-", "");
      mockImages.set(draftId, dataURL);
      return { draftId, displayName, mime, width: 1, height: 1, bytes: dataURL.length };
    },
    async ReadDraftImageForTab(_tabID: string, id: string) {
      const image = mockImages.get(id);
      if (!image) throw new Error("draft missing");
      return image;
    },
    async CaptureAttachmentTarget(target: ComposerTarget) {
      return { token: target.tabId || target.draftId || "mock-attachment-target", capabilities: ["attachments-v2"] };
    },
    async ReleaseAttachmentTarget(_token: string) {},
    async StageImageForTarget(_token: string, _operation: string, displayName: string, mime: string, dataURL: string) {
      const draftId = crypto.randomUUID().replaceAll("-", "");
      mockImages.set(draftId, dataURL);
      return { draftId, displayName, mime, width: 1, height: 1, bytes: dataURL.length };
    },
    async ReadDraftImageForTarget(_token: string, id: string) { const image = mockImages.get(id); if (!image) throw new Error("draft missing"); return image; },
    async ReleaseDraftImageForTarget(_token: string, id: string) { mockImages.delete(id); },
    async ReleaseDraftImageForTab(_tabID: string, id: string) { mockImages.delete(id); },
    async SavePastedImage(dataURL: string) {
      const path = `.reasonix/attachments/mock-${mockImages.size + 1}.png`;
      mockImages.set(path, dataURL);
      return path;
    },
    async SavePastedImageForComposerTarget(_target: ComposerTarget, dataURL: string) { return this.SavePastedImage(dataURL); },
    async SavePastedImageForTab(_tabID: string, dataURL: string) { return this.SavePastedImage(dataURL); },
    async SavePastedFile(name: string, dataURL: string) {
      const path = `.reasonix/attachments/mock-${name}`;
      mockImages.set(path, dataURL);
      return path;
    },
    async SavePastedFileForComposerTarget(_target: ComposerTarget, name: string, dataURL: string) { return this.SavePastedFile(name, dataURL); },
    async SavePastedFileForTarget(token: string, name: string, dataURL: string) { return this.SavePastedFileForTab(token,name,dataURL); },
    async SavePastedFileForTab(_tabID: string, name: string, dataURL: string) { return this.SavePastedFile(name, dataURL); },
    async SaveClipboardImage() {
      const path = `.reasonix/attachments/mock-clipboard-${mockImages.size + 1}.png`;
      mockImages.set(path, mockPreviewImageDataURL);
      return path;
    },
    async SaveClipboardImageForComposerTarget(_target: ComposerTarget) { return this.SaveClipboardImage(); },
    async SaveClipboardImageForTarget(token: string) { return this.SaveClipboardImageForTab(token); },
    async SaveClipboardImageForTab(_tabID: string) { return this.SaveClipboardImage(); },
    async AttachmentDataURL(path: string) { return mockImages.get(path) ?? mockPreviewImageDataURL; },
    async AttachmentDataURLForComposerTarget(_target: ComposerTarget, path: string) { return this.AttachmentDataURL(path); },
    async AttachmentDataURLForTarget(token: string, path: string) { return this.AttachmentDataURLForTab(token,path); },
    async AttachmentDataURLForTab(_tabID: string, path: string) { return this.AttachmentDataURL(path); },
    async AttachDropped(path: string) {
      const name = path.split(/[/\\]/).filter(Boolean).pop() ?? path;
      if (!/\.\w{1,6}$/i.test(name)) {
        const tokenName = name.replace(/[^\w.-]+/g, "-") || "folder";
        return { kind: "workspace", path: `__reasonix_external_folder/mock/${tokenName}`, isDir: true, displayPath: path };
      }
      const attachmentPath = `.reasonix/attachments/mock-${name}`;
      mockImages.set(attachmentPath, mockPreviewImageDataURL);
      return { kind: "attachment", path: attachmentPath };
    },
    async AttachDroppedForComposerTarget(_target: ComposerTarget, path: string) { return this.AttachDropped(path); },
    async AttachDroppedForTarget(token: string, path: string) { return this.AttachDroppedForTab(token,path); },
    async AttachDroppedForTab(_tabID: string, path: string) { return this.AttachDropped(path); },
    async ReadSessionAttachmentForTab(_tabID: string, digest: string, offset: number) {
      const image = mockImages.get(digest) ?? "data:image/png;base64,";
      const data = image.split(",", 2)[1] ?? "";
      return { data: offset === 0 ? data : "", nextOffset: data.length, done: true };
    },
    async StartTurnForTabWithDrafts(tabID: string, input: string, submissionID: string) {
      if (this.StartTurnForTab) {
        const receipt = await this.StartTurnForTab(tabID, input, submissionID);
        return { turnId: receipt.turnId, status: receipt.status, submissionId: receipt.submissionId || submissionID };
      }
      await this.SubmitToTabWithID(tabID, input, submissionID);
      return { turnId: submissionID, status: "queued", submissionId: submissionID };
    },
    async StartTurnForAttachmentTarget(token: string, submissionID: string, request: {input:string;display:string}) {
      await this.SubmitDisplayToTabWithID(token,request.display,request.input,submissionID);
      return {turnId:submissionID,status:"queued",submissionId:submissionID};
    },
    async EnqueueForAttachmentTarget(token: string, id: string, input: string, display: string) { return this.EnqueueInboxFollowup(token,display,input,id); },
  };
}

export function callMockAttachment(
  owner: AppBindings,
  name: keyof AttachmentBindings,
  args: unknown[],
): unknown {
  const method = makeMockAttachmentBindings()[name] as (...values: unknown[]) => unknown;
  return method.apply(owner, args);
}
