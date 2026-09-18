export function appendHistoryAttachmentRefs(
  text: string,
  items?: Array<{ digest?: string; name?: string; mime?: string }>,
): string {
  return text + (items?.map(item => item.digest
    ? ` @[${item.name || "image"}](attachment:${item.digest})`
    : "").join("") || "");
}
