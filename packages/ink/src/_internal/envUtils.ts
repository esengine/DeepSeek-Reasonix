// Env vars are strings or undefined. Accept the common truthy spellings users
// reach for instinctively — "1", "true", "yes", "on" — so feature flags work
// regardless of which convention the caller picked up from another tool.
export function isEnvTruthy(value: string | undefined | null): boolean {
  if (!value) return false;
  const normalized = value.trim().toLowerCase();
  return (
    normalized === '1' ||
    normalized === 'true' ||
    normalized === 'yes' ||
    normalized === 'on'
  );
}
