import type { AppBindings } from "./bridge";
import type { StructuredInvocationSubmit } from "./invocationDisplay";
import type { ComposerTarget } from "../generated/desktopContract.generated";

const pendingImageSubmissions = new Map<string, { fingerprint: string; id: string }>();

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
  attachments: Array<{ draftId?: string; clientAttachmentId?: string }>,
  structured: StructuredInvocationSubmit | undefined,
  display: string,
  input: string,
) {
  const token = await captureImageTarget(app, target);
  const submissionId = imageSubmissionIdentity(draftKey, fingerprint);
  return {
    token,
    submissionId,
    structured: {
      display: structured?.display ?? display,
      input: structured?.input ?? input,
      invocations: structured?.invocations ?? [],
      attachmentTarget: token,
      attachmentSubmissionId: submissionId,
      attachments: attachments.map((item, index) => ({
        clientAttachmentId: item.clientAttachmentId || `image-${index + 1}`,
        draftId: item.draftId,
      })),
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

export async function stageImageFile(app: AppBindings, target: string, draftKey: string, file: File) {
  const dataURL = await readFileAsDataURL(file);
  const staged = await app.StageImageForTarget!(target, `${draftKey}:${file.name}:${file.lastModified}`, file.name, file.type, dataURL);
	const path = staged.draftId ? `draft:${staged.draftId}` : staged.path;
	if (!path) throw new Error("attachment staging returned no source");
	const previewUrl = staged.draftId
		? await app.ReadDraftImageForTarget!(target, staged.draftId)
		: await app.AttachmentDataURLForTarget!(target, path);
	return { path, previewUrl, displayName: file.name, draftId: staged.draftId || undefined, file };
}
