import { noGrant } from "./errors.js";

export interface BrowserGrant {
  grantId: string;
  taskId: string;
  sessionId: string;
  generation: string;
  createdAt: number;
}

export interface GrantRegistryDeps {
  generation(): string;
  now?(): number;
  onRevoked?(grant: BrowserGrant): void;
}

// Grants are minted by Go per task (its "tabId") and die with the service
// generation that minted them, so a restarted service can never act on a
// grant it does not remember.
export class GrantRegistry {
  private readonly grants = new Map<string, BrowserGrant>();
  private generation = "";

  constructor(private readonly deps: GrantRegistryDeps) {}

  install(input: { grantId: string; taskId: string; sessionId: string }): BrowserGrant {
    if (input.grantId === "" || input.taskId === "") throw noGrant("grantId and tabId are required");
    const generation = this.deps.generation();
    if (generation === "") throw noGrant("desktop service is not running");
    this.generation = generation;
    const grant: BrowserGrant = { ...input, generation, createdAt: (this.deps.now ?? Date.now)() };
    this.grants.set(input.grantId, grant);
    return grant;
  }

  revoke(grantId: string): BrowserGrant | null {
    const grant = this.grants.get(grantId);
    if (!grant) return null;
    this.grants.delete(grantId);
    this.deps.onRevoked?.(grant);
    return grant;
  }

  // A generation change (service restart) retires every grant at once.
  observeGeneration(generation: string): void {
    if (generation === this.generation) return;
    this.generation = generation;
    for (const grantId of [...this.grants.keys()]) this.revoke(grantId);
  }

  verify(grantId: string): BrowserGrant {
    const grant = this.grants.get(grantId);
    if (!grant) throw noGrant(`unknown grant ${grantId || "(empty)"}`);
    if (grant.generation !== this.deps.generation()) {
      this.revoke(grantId);
      throw noGrant("grant belongs to an earlier service generation");
    }
    return grant;
  }

  // The tab must belong to the grant's task; a grant never reaches across.
  verifyTab(grantId: string, tabTaskId: string | undefined): BrowserGrant {
    const grant = this.verify(grantId);
    if (tabTaskId === undefined) throw noGrant("unknown browser tab");
    if (tabTaskId !== grant.taskId) throw noGrant("browser tab belongs to another task");
    return grant;
  }

  get size(): number {
    return this.grants.size;
  }
}
