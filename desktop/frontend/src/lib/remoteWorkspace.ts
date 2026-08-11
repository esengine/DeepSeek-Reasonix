/**
 * Resolve the workspace used to open a remote Serve page.
 *
 * A host's last-opened workspace wins over its configured default. Fresh
 * hosts fall back to the SSH login user's home directory, which the remote
 * bootstrap resolves from `~` before launching Serve.
 */
export function resolveRemoteWorkspace(lastWorkspace?: string, defaultWorkspace?: string): string {
  return lastWorkspace?.trim() || defaultWorkspace?.trim() || "~";
}

/**
 * Host-scoped latest-request-wins generations for remote window launches.
 * Requests for one SSH host supersede only that host; different hosts remain
 * independent because each opens in its own native window.
 */
export class RemoteWorkspaceLaunchGate {
  private readonly generations = new Map<string, number>();

  begin(hostId: string): number {
    const next = (this.generations.get(hostId) ?? 0) + 1;
    this.generations.set(hostId, next);
    return next;
  }

  isCurrent(hostId: string, generation: number): boolean {
    return this.generations.get(hostId) === generation;
  }
}
