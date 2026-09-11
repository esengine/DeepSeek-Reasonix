/** Model-facing Auto Guard policy. Must not render as a user-side steer. */
const HOST_RECOVERY_PREFIXES = [
  "A tool failed. Use read-only diagnosis as needed",
  "The tool timed out or hit a transient execution limit.",
];

export function isHostRecoveryGuidance(text: string): boolean {
  const trimmed = text.trim();
  const body = trimmed.startsWith("↪ ") ? trimmed.slice("↪ ".length).trimStart() : trimmed;
  return HOST_RECOVERY_PREFIXES.some((prefix) => body.startsWith(prefix));
}
