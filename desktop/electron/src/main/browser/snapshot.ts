import { randomToken, type DocumentRegistry, type FrameBinding } from "./documents.js";
import type { GuestFrame, GuestPage } from "./guestView.js";
import { scriptCall } from "./pageScripts.js";
import { SNAPSHOT_SCRIPT_SOURCE, type SnapshotOutput } from "./snapshotScript.js";

export const REGISTRY_KEY = "__reasonixBrowserRegistry";
export const ISOLATED_WORLD = 1;
export const MAX_SNAPSHOT_NODES = 4000;

export interface SnapshotResult {
  documentToken: string;
  url: string;
  title: string;
  tree: string;
  refs: number;
}

// Electron 44 offers an isolated world only on the WebContents (main frame);
// WebFrameMain.executeJavaScript runs in the frame's own main world, so child
// frame registries live there and a hostile page can at most stale itself.
export function runInFrame(page: GuestPage, frame: GuestFrame, code: string): Promise<unknown> {
  if (frame.frameTreeNodeId === page.mainFrame.frameTreeNodeId) return page.executeJavaScriptInIsolatedWorld(ISOLATED_WORLD, [{ code }]);
  return frame.executeJavaScript(code);
}

export function findFrame(page: GuestPage, frameTreeNodeId: number): GuestFrame | null {
  const main = page.mainFrame;
  if (main.frameTreeNodeId === frameTreeNodeId) return main;
  for (const frame of main.framesInSubtree) {
    if (frame.frameTreeNodeId === frameTreeNodeId && !frame.detached) return frame;
  }
  return null;
}

function isSnapshotOutput(value: unknown): value is SnapshotOutput {
  if (typeof value !== "object" || value === null) return false;
  const out = value as Record<string, unknown>;
  return typeof out.docId === "string" && typeof out.tree === "string" && typeof out.refs === "number" && typeof out.nodes === "number";
}

function clipURL(url: string): string {
  return url.length > 120 ? `${url.slice(0, 119)}…` : url;
}

function indent(tree: string): string {
  return tree.split("\n").map((line) => `  ${line}`).join("\n");
}

export async function takeSnapshot(page: GuestPage, tabId: string, epoch: number, selector: string, documents: DocumentRegistry): Promise<SnapshotResult> {
  const snapshotId = randomToken(8);
  const main = page.mainFrame;
  const mainRaw = await runInFrame(page, main, scriptCall(SNAPSHOT_SCRIPT_SOURCE, { key: REGISTRY_KEY, snapshotId, prefix: "", selector, budget: MAX_SNAPSHOT_NODES }));
  if (!isSnapshotOutput(mainRaw)) throw new Error("snapshot script returned no tree");
  const frames: FrameBinding[] = [{ prefix: "", frameTreeNodeId: main.frameTreeNodeId, docId: mainRaw.docId }];
  const sections = [mainRaw.tree];
  let refs = mainRaw.refs;
  let budget = MAX_SNAPSHOT_NODES - mainRaw.nodes;
  let index = 0;
  for (const frame of main.framesInSubtree) {
    if (frame.frameTreeNodeId === main.frameTreeNodeId || frame.detached) continue;
    if (budget <= 0) break;
    index += 1;
    const prefix = `f${index}`;
    let raw: unknown;
    try {
      raw = await frame.executeJavaScript(scriptCall(SNAPSHOT_SCRIPT_SOURCE, { key: REGISTRY_KEY, snapshotId, prefix, selector: "", budget }));
    } catch {
      continue;
    }
    if (!isSnapshotOutput(raw)) continue;
    frames.push({ prefix, frameTreeNodeId: frame.frameTreeNodeId, docId: raw.docId });
    refs += raw.refs;
    budget -= raw.nodes;
    if (raw.tree === "") continue;
    sections.push(`frame ${prefix} ${JSON.stringify(clipURL(frame.url))}\n${indent(raw.tree)}`);
  }
  const documentToken = documents.issue({ tabId, epoch, snapshotId, frames });
  return { documentToken, url: page.getURL(), title: page.getTitle(), tree: sections.join("\n"), refs };
}
