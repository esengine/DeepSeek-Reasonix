import { randomBytes } from "node:crypto";

export interface FrameBinding {
  prefix: string;
  frameTreeNodeId: number;
  docId: string;
}

// One snapshot of one document version. The token is the only handle Go ever
// sees; the snapshotId is what the page-side registry was stamped with.
export interface DocumentBinding {
  tabId: string;
  epoch: number;
  snapshotId: string;
  frames: FrameBinding[];
}

export interface ParsedRef {
  prefix: string;
  ref: string;
}

const REF_PATTERN = /^(f\d+)?(e\d+)$/;

export function parseRef(ref: string): ParsedRef | null {
  const match = REF_PATTERN.exec(ref);
  if (!match) return null;
  return { prefix: match[1] ?? "", ref };
}

export function randomToken(bytes = 16): string {
  return randomBytes(bytes).toString("hex");
}

export class DocumentRegistry {
  private readonly byToken = new Map<string, DocumentBinding>();
  private readonly byTab = new Map<string, string>();

  constructor(private readonly mint: () => string = randomToken) {}

  // A new snapshot replaces the tab's previous token: refs from an older
  // snapshot must fail as stale rather than land on a re-rendered page.
  issue(binding: DocumentBinding): string {
    this.invalidateTab(binding.tabId);
    const token = this.mint();
    this.byToken.set(token, binding);
    this.byTab.set(binding.tabId, token);
    return token;
  }

  // After an action left the document intact the same refs stay valid, so
  // the binding is re-issued under a fresh token and the old one retired.
  rotate(token: string): string | null {
    const binding = this.byToken.get(token);
    if (!binding) return null;
    this.byToken.delete(token);
    const next = this.mint();
    this.byToken.set(next, binding);
    this.byTab.set(binding.tabId, next);
    return next;
  }

  lookup(token: string): DocumentBinding | undefined {
    return this.byToken.get(token);
  }

  currentToken(tabId: string): string | undefined {
    return this.byTab.get(tabId);
  }

  invalidateTab(tabId: string): void {
    const token = this.byTab.get(tabId);
    if (token !== undefined) this.byToken.delete(token);
    this.byTab.delete(tabId);
  }

  clear(): void {
    this.byToken.clear();
    this.byTab.clear();
  }
}
