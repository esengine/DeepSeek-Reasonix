import { parseRef, type DocumentBinding, type FrameBinding } from "./documents.js";
import { staleReference } from "./errors.js";
import type { GuestFrame, GuestPage } from "./guestView.js";
import { LOCATE_SCRIPT_SOURCE, RESOLVE_SCRIPT_SOURCE, scriptCall, type LocateOutput, type ResolvedElement, type ResolveOutput } from "./pageScripts.js";
import { findFrame, REGISTRY_KEY, runInFrame } from "./snapshot.js";

export interface RefFrame {
  frame: GuestFrame;
  binding: FrameBinding;
  isMainFrame: boolean;
}

export interface ResolvedRef extends RefFrame {
  element: ResolvedElement;
}

export interface LocatedRef extends RefFrame {
  ref: string;
  snapshotId: string;
  tag: string;
  type: string;
  path: string;
}

export type RefOutcome<T> = { ok: true; value: T } | { ok: false; reason: string };

function hasOk(value: unknown): value is { ok: boolean; reason?: string } {
  return typeof value === "object" && value !== null && typeof (value as { ok?: unknown }).ok === "boolean";
}

export function frameForRef(page: GuestPage, binding: DocumentBinding, ref: string): RefFrame {
  const parsed = parseRef(ref);
  if (!parsed) throw staleReference(`malformed ref ${JSON.stringify(ref)}`);
  const frameBinding = binding.frames.find((entry) => entry.prefix === parsed.prefix);
  if (!frameBinding) throw staleReference(`ref ${ref} names a frame the snapshot did not see`);
  const frame = findFrame(page, frameBinding.frameTreeNodeId);
  if (!frame) throw staleReference(`frame of ${ref} is gone`);
  return { frame, binding: frameBinding, isMainFrame: frame.frameTreeNodeId === page.mainFrame.frameTreeNodeId };
}

async function runRefScript(page: GuestPage, target: RefFrame, source: string, snapshotId: string, ref: string, extra: Record<string, unknown>): Promise<unknown> {
  try {
    return await runInFrame(page, target.frame, scriptCall(source, { key: REGISTRY_KEY, snapshotId, docId: target.binding.docId, ref, ...extra }));
  } catch (error) {
    throw staleReference(`frame of ${ref} cannot run scripts: ${String(error)}`);
  }
}

// Resolves a snapshot ref to viewport geometry inside the frame that produced
// it. Stale refs throw -32010; a live ref that cannot be acted on reports why.
export async function resolveRef(page: GuestPage, binding: DocumentBinding, ref: string, scroll: boolean): Promise<RefOutcome<ResolvedRef>> {
  const target = frameForRef(page, binding, ref);
  const raw = await runRefScript(page, target, RESOLVE_SCRIPT_SOURCE, binding.snapshotId, ref, { scroll });
  if (!hasOk(raw)) throw staleReference(`resolver returned nothing for ${ref}`);
  const out = raw as ResolveOutput;
  if (!out.ok) {
    if (out.reason === "stale") throw staleReference(`${ref} predates the current document`);
    return { ok: false, reason: out.reason };
  }
  if (!out.frameOffsetKnown) return { ok: false, reason: "element sits in a cross-origin frame; its position cannot be determined" };
  return { ok: true, value: { ...target, element: out } };
}

export async function locateRef(page: GuestPage, binding: DocumentBinding, ref: string): Promise<RefOutcome<LocatedRef>> {
  const target = frameForRef(page, binding, ref);
  const raw = await runRefScript(page, target, LOCATE_SCRIPT_SOURCE, binding.snapshotId, ref, {});
  if (!hasOk(raw)) throw staleReference(`locator returned nothing for ${ref}`);
  const out = raw as LocateOutput;
  if (!out.ok) {
    if (out.reason === "stale") throw staleReference(`${ref} predates the current document`);
    return { ok: false, reason: out.reason };
  }
  return { ok: true, value: { ...target, ref, snapshotId: binding.snapshotId, tag: out.tag, type: out.type, path: out.path } };
}
