import { download } from "../port/download";
import { host } from "../port/host";
import type { HubPort, TreeSession } from "../port/hub";
import { toMarkdown } from "./sessionExportMarkdown";

export async function saveSessionFile(
  hub: Pick<HubPort, "exportSession">,
  session: TreeSession,
  format: "json" | "md" = "json",
) {
  const exported = await hub.exportSession(session.path);
  let nativeArchive: { records?: unknown[] } | undefined;
  try {
    const parsed = JSON.parse(exported.content) as { format?: string; records?: unknown[] };
    if (parsed.format === "reasonix-native-session") nativeArchive = parsed;
  } catch {}
  // A native archive keeps its messages in `records`, not one per line.
  const records = nativeArchive
    ? (nativeArchive.records ?? [])
    : exported.content
        .split(/\r?\n/)
        .filter((line) => line.trim())
        .map((line) => JSON.parse(line) as unknown);
  const title = session.title || session.name;
  const content =
    format === "md"
      ? toMarkdown(title, records)
      : JSON.stringify(
          nativeArchive ?? { version: 1, title, exportedAt: new Date().toISOString(), records },
          null,
          2,
        );
  const clean = (session.title || exported.name || session.name).replace(/[<>:"/\\|?*\x00-\x1f]/g, "-").trim() || "reasonix-session";
  const filename = `${clean}.${format}`;
  const saved = await host().saveText(filename, content);
  if (saved === null) download(filename, content);
}
