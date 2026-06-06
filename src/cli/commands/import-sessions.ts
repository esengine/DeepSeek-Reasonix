import { type ExternalSessionSource, importExternalSession } from "../../session-import.js";

export interface ImportSessionsOptions {
  source: string;
  path: string;
  name?: string;
  workspace?: string;
  summary?: string;
  force?: boolean;
}

export function importSessionsCommand(opts: ImportSessionsOptions): void {
  const source = normalizeSource(opts.source);
  if (!source) {
    console.error(`unsupported source "${opts.source}" (expected: claude or codex).`);
    process.exit(1);
  }
  try {
    const result = importExternalSession({
      source,
      path: opts.path,
      name: opts.name,
      workspace: opts.workspace,
      summary: opts.summary,
      force: opts.force,
    });
    console.log(`imported ${result.messageCount} message(s) into session "${result.name}"`);
    console.log(`source: ${result.source}`);
    console.log(`file:   ${result.path}`);
    if (result.workspace) console.log(`workspace: ${result.workspace}`);
    if (result.summary) console.log(`summary:   ${result.summary}`);
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    console.error(message);
    if (message.startsWith("target session already exists:")) {
      console.error("re-run with --force to overwrite, or pass --name <session>.");
    }
    process.exit(1);
  }
}

function normalizeSource(value: string): ExternalSessionSource | undefined {
  return value === "claude" || value === "codex" ? value : undefined;
}
