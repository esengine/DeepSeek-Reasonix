/** Hands the composer buffer to $EDITOR / $VISUAL / $GIT_EDITOR and reads back what the user saved. */

import { spawn } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { t } from "../../i18n/index.js";

export interface OpenEditorResult {
  /** "ok" when the editor returned 0 and we read content back; "missing" when no editor env var is set; "failed" on spawn error or non-zero exit. */
  kind: "ok" | "missing" | "failed";
  /** Final buffer contents. On `missing` / `failed`, this is the original `initial` (caller restores composer state). */
  content: string;
  /** Human-readable detail — surface to the user on failed / missing. */
  detail?: string;
}

/** $VISUAL beats $EDITOR per traditional Unix precedence; $GIT_EDITOR wins because users who set it are explicit about their tool of choice. */
export function detectEditor(env: NodeJS.ProcessEnv = process.env): string | null {
  for (const key of ["GIT_EDITOR", "VISUAL", "EDITOR"]) {
    const raw = env[key];
    if (typeof raw === "string" && raw.trim().length > 0) return raw.trim();
  }
  return null;
}

/** Writes `initial` to a temp file, spawns the editor on it, reads back on exit. Strips one trailing newline that most editors auto-append. */
export async function openInExternalEditor(initial: string): Promise<OpenEditorResult> {
  const editor = detectEditor();
  if (!editor) {
    return {
      kind: "missing",
      content: initial,
      detail: t("composer.editorMissing"),
    };
  }
  const dir = mkdtempSync(join(tmpdir(), "reasonix-compose-"));
  const path = join(dir, "REASONIX_INPUT.md");
  try {
    writeFileSync(path, initial, "utf8");
    await spawnEditor(editor, path);
    const raw = readFileSync(path, "utf8");
    return { kind: "ok", content: normalizeEditorBuffer(raw) };
  } catch (err) {
    return {
      kind: "failed",
      content: initial,
      detail: t("composer.editorExited", { code: (err as Error).message }),
    };
  } finally {
    try {
      rmSync(dir, { recursive: true, force: true });
    } catch {
      /* leftover temp file is harmless — OS cleans tmpdir periodically */
    }
  }
}

function spawnEditor(editor: string, path: string): Promise<void> {
  return new Promise((resolve, reject) => {
    // Use a shell so `editor` strings like `nvim --noplugin` or `code --wait`
    // split correctly across platforms. `stdio: inherit` is what lets vim /
    // nano take over the terminal — they need TTY input + output.
    const child = spawn(`${editor} "${path}"`, {
      shell: true,
      stdio: "inherit",
    });
    child.on("error", reject);
    child.on("exit", (code) => {
      if (code === 0 || code === null) resolve();
      else reject(new Error(String(code)));
    });
  });
}

export function normalizeEditorBuffer(s: string): string {
  const normalized = s.replace(/\r\n?/g, "\n");
  if (normalized.endsWith("\n")) return normalized.slice(0, -1);
  return normalized;
}
