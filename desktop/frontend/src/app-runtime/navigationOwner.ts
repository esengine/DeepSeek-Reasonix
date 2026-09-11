export type WorkspaceNavigationPorts = {
  claimIntent: () => number;
  beginSurface: (intent: number) => void;
  isIntentCurrent: (intent: number) => boolean;
  pickWorkspace: (intent: number) => Promise<string>;
  switchWorkspace: (path: string, intent: number) => Promise<string>;
  markProjectChanged: (updater: (value: number) => number) => void;
  refreshTabsAfterMutation: (latest: () => boolean) => Promise<unknown>;
  maskTarget: (intent: number) => void;
};

/** Source-bound workspace navigation executor with one terminal surface owner. */
export async function navigateWorkspace(
  path: string | undefined,
  ports: WorkspaceNavigationPorts,
): Promise<string> {
  const intent = ports.claimIntent();
  ports.beginSurface(intent);
  try {
    const picked = path === undefined
      ? await ports.pickWorkspace(intent)
      : await ports.switchWorkspace(path, intent);
    if (!ports.isIntentCurrent(intent)) return picked;
    if (picked) {
      ports.markProjectChanged((value) => value + 1);
      await ports.refreshTabsAfterMutation(() => ports.isIntentCurrent(intent));
    }
    return picked;
  } finally {
    // Masking is intent-matched by the surface owner, so an old finally cannot
    // release or advance a replacement request.
    ports.maskTarget(intent);
  }
}
