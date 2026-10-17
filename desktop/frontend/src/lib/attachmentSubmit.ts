import type { AppBindings } from "./bridge";
import type { StructuredInvocationSubmit } from "./invocationDisplay";
import type { ComposerTarget } from "../generated/desktopContract.generated";
import type { WorkspaceReference } from "./composerDraftTypes";

export async function restoreExternalFolderReferences(app: AppBindings, target: ComposerTarget, refs: WorkspaceReference[]) {
  const external = refs.filter(isExternalWorkspaceReference);
  if (!external.length) return;
  const token = await captureAttachmentTarget(app, target, ["AttachDroppedForTarget"]);
  try {
    for (const ref of external) {
      if (!ref.displayPath) throw new Error("External folder source is unavailable; reattach the folder before sending");
      const restored = await app.AttachDroppedForTarget!(token, ref.displayPath);
      if (restored.kind !== "workspace" || Boolean(restored.isDir) !== Boolean(ref.isDir) || restored.path !== ref.path) {
        throw new Error("External folder changed; reattach the folder before sending");
      }
    }
  } finally { await app.ReleaseAttachmentTarget?.(token); }
}

export function isExternalWorkspaceReference(ref: WorkspaceReference): boolean {
  return ref.path.startsWith("__reasonix_external_folder/") || Boolean(!ref.isDir && ref.displayPath && /^(?:\/|[a-zA-Z]:[\\/]|\\\\)/.test(ref.path));
}

const pendingImageSubmissions = new Map<string, { fingerprint: string; id: string }>();

// Native files remain path references; pathless files send a raw HTTP body.
export function attachmentUploadFailure(error: unknown, fallback: string, formatLimit?: (bytes: number) => string): string {
  return isAttachmentUploadLimitError(error) ? formatLimit?.(error.limit_bytes) ?? error.message : fallback;
}

export function isAttachmentUploadLimitError(error: unknown): error is Error & { code: "attachment.too_large"; limit_bytes: number } {
  return error instanceof Error && "code" in error && error.code === "attachment.too_large"
    && "limit_bytes" in error && typeof error.limit_bytes === "number" && Number.isFinite(error.limit_bytes) && error.limit_bytes > 0;
}

export async function attachOrdinaryFile(
  app: AppBindings, token: string, file: File,
  getPathForFile?: (file: File) => string, fetcher: typeof fetch = fetch,
): Promise<{ kind: string; path: string; isDir?: boolean; displayPath?: string }> {
  const nativePath = getPathForFile?.(file);
  if (nativePath) {
    if (!app.AttachDroppedForTarget) throw new Error("unsupported: attachments-v2");
    return app.AttachDroppedForTarget(token, nativePath);
  }
  const query = new URLSearchParams({ target: token, name: file.name });
  const response = await fetcher(`/__reasonix_attachment_upload?${query}`, {
    method: "POST", headers: { "content-type": "application/octet-stream" }, body: file,
  });
  if (!response.ok) {
    const error: unknown = await response.json().catch(() => null);
    if (error && typeof error === "object" && "code" in error && error.code === "attachment.too_large"
      && "message" in error && typeof error.message === "string"
      && "limit_bytes" in error && typeof error.limit_bytes === "number"
      && "limit_source" in error && typeof error.limit_source === "string") {
      throw Object.assign(new Error(error.message), { code: error.code, limit_bytes: error.limit_bytes, limit_source: error.limit_source });
    }
    throw new Error(`File upload failed (${response.status})`);
  }
  const value: unknown = await response.json();
  if (!value || typeof value !== "object" || !("path" in value) || typeof value.path !== "string" || !value.path) {
    throw new Error("File upload returned no attachment path");
  }
  return { kind: "attachment", path: value.path };
}

export function imageSubmissionIdentity(draft: string, fingerprint: string): string {
  const previous = pendingImageSubmissions.get(draft);
  if (previous?.fingerprint === fingerprint) return previous.id;
  const id = `image-${crypto.randomUUID()}`;
  pendingImageSubmissions.set(draft, { fingerprint, id });
  return id;
}

export function settleImageSubmission(draft: string, id?: string): void {
  if (pendingImageSubmissions.get(draft)?.id === id) pendingImageSubmissions.delete(draft);
}

export async function prepareImageSubmission(
  app: AppBindings,
  target: ComposerTarget,
  draftKey: string,
  fingerprint: string,
  attachments: Array<{ draftId?: string; clientAttachmentId?: string; recoveryPath?: string; displayName?: string }>,
  structured: StructuredInvocationSubmit | undefined,
  display: string,
  input: string,
) {
  const token = await captureImageTarget(app, target);
  const submissionId = imageSubmissionIdentity(draftKey, fingerprint);
  const restored = [];
  try {
    for (const [index, item] of attachments.entries()) {
      let draftId = item.draftId;
      if (!draftId && item.recoveryPath) {
        if (!app.AttachmentDataURLForTarget || !app.StageImageForTarget) throw new Error("unsupported: attachments-v2");
        const data = await app.AttachmentDataURLForTarget(token, item.recoveryPath);
        const staged = await app.StageImageForTarget(token, `${submissionId}:restore:${index}`, item.displayName || "image", "", data);
        draftId = staged.draftId;
      }
      if (!draftId) throw new Error("Image source is unavailable; reattach the image before sending");
      restored.push({ clientAttachmentId: item.clientAttachmentId || `image-${index + 1}`, draftId });
    }
  } catch (error) {
    await app.ReleaseAttachmentTarget?.(token).catch(() => {});
    throw error;
  }
  return {
    token,
    submissionId,
    structured: {
      display: structured?.display ?? display,
      input: structured?.input ?? input,
      invocations: structured?.invocations ?? [],
      attachmentTarget: token,
      attachmentSubmissionId: submissionId,
      attachments: restored,
    } satisfies StructuredInvocationSubmit,
  };
}

export function submitAttachmentTurn(app: AppBindings, submissionId: string, structured: StructuredInvocationSubmit, original: string, initialGoal?: { goal: string; toolApprovalMode?: string }) {
  if (!app.StartTurnForAttachmentTarget || !structured.attachmentTarget) throw new Error("unsupported: attachments-v2");
  return app.StartTurnForAttachmentTarget(structured.attachmentTarget, submissionId, {
    input: structured.input, display: structured.display, original,
    goal: initialGoal?.goal, toolApprovalMode: initialGoal?.toolApprovalMode,
    invocations: structured.invocations, attachments: structured.attachments ?? [],
  });
}

export function readFileAsDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(file);
  });
}

export async function captureImageTarget(app: AppBindings, target: ComposerTarget): Promise<string> {
  return captureAttachmentTarget(app, target, target.kind === "draft"
    ? ["StageImageForTarget", "AttachmentDataURLForTarget"]
    : ["StageImageForTarget", "ReadDraftImageForTarget"]);
}

export async function captureAttachmentTarget(
  app: AppBindings,
  target: ComposerTarget,
  required: ReadonlyArray<keyof AppBindings>,
): Promise<string> {
  if (!app.CaptureAttachmentTarget || required.some((name) => typeof app[name] !== "function")) {
    throw new Error("unsupported: attachments-v2");
  }
  const captured = await app.CaptureAttachmentTarget(target);
  if (!captured.capabilities.includes("attachments-v2")) throw new Error("unsupported: attachments-v2");
  return captured.token;
}

export async function stageImageFile(app: AppBindings, target: string, draftKey: string, file: File, persistentTarget?: ComposerTarget) {
  const dataURL = await readFileAsDataURL(file);
  const staged = await app.StageImageForTarget!(target, `${draftKey}:${file.name}:${file.lastModified}`, file.name, file.type, dataURL);
	const path = staged.draftId ? `draft:${staged.draftId}` : staged.path;
	if (!path) throw new Error("attachment staging returned no source");
	const previewUrl = staged.draftId
		? await app.ReadDraftImageForTarget!(target, staged.draftId)
		: await app.AttachmentDataURLForTarget!(target, path);
	const recoveryPath = persistentTarget && staged.draftId ? await app.SavePastedImageForComposerTarget(persistentTarget, dataURL) : undefined;
	return { path, previewUrl, displayName: file.name, draftId: staged.draftId || undefined, recoveryPath, file };
}
