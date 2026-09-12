
/**
 * History-backed items carry ids derived from their backend entry
 * (`he:<entryId>`, tool calls `he:<entryId>:tc<index>`, or a bare toolCallId).
 * Returns the entryId for rows that may carry unresolved lazy-content refs.
 */
export function historyEntryIdForItemId(id: string | undefined): string | undefined {
  if (id?.startsWith("m:") || id?.startsWith("record:")) return id;
  if (!id || !id.startsWith("he:")) return undefined;
  return id.slice(3).replace(/:tc\d+$/, "");
}
