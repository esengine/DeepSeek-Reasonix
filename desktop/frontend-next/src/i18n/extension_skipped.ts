// The typed payload a skipped-extension notice carries as its detail, read as
// the variables its sentence is worded from. Anything that does not parse reads
// as no payload, so the notice falls back to the kernel's own text.
export const EXTENSION_SKIPPED = "extension_skipped";

export function extensionSkippedVars(detail: string | undefined): { ext: string; point: string } | undefined {
  if (!detail) return undefined;
  try {
    const p = JSON.parse(detail) as { extension?: unknown; point?: unknown };
    if (typeof p.extension === "string" && p.extension !== "" && typeof p.point === "string") return { ext: p.extension, point: p.point };
  } catch {
    return undefined;
  }
  return undefined;
}
