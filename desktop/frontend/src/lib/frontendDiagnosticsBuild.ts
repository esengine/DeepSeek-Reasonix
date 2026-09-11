export function isFrontendDiagnosticsBuild(channel: string, development: boolean): boolean {
  // Preview/canary are the repository's non-stable test artifact channels;
  // stable builds must never expose a recorder entry in the product UI.
  return development || channel === "test" || channel === "preview" || channel === "canary";
}
