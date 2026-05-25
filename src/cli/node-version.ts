export const MIN_NODE_MAJOR = 22;

export function parseNodeMajor(version: string): number | null {
  const major = Number.parseInt(version.split(".")[0] ?? "", 10);
  return Number.isInteger(major) ? major : null;
}

export function isSupportedNodeVersion(
  version = process.versions.node,
  minMajor = MIN_NODE_MAJOR,
): boolean {
  const major = parseNodeMajor(version);
  return major !== null && major >= minMajor;
}

export function unsupportedNodeMessage(
  version = process.versions.node,
  minMajor = MIN_NODE_MAJOR,
): string {
  return `Reasonix requires Node ${minMajor}+ (current: ${version}). Install Node ${minMajor} or newer and rerun reasonix.`;
}

export function enforceSupportedNodeVersion(
  version = process.versions.node,
  minMajor = MIN_NODE_MAJOR,
): void {
  if (isSupportedNodeVersion(version, minMajor)) return;
  process.stderr.write(`${unsupportedNodeMessage(version, minMajor)}\n`);
  process.exit(1);
}
