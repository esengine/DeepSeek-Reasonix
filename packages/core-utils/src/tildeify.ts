import { homedir } from "node:os";

/** Replace the user's home directory with `~` for display purposes.
 *  Mirrors the shell's `~` expansion in reverse. */
export function tildeify(p: string): string {
  const home = homedir();
  if (!home) return p;
  const normalized = home.replace(/[\\/]+$/, "");
  if (p === normalized) return "~";
  if (p.startsWith(`${normalized}/`)) return `~/${p.slice(normalized.length + 1).replace(/^[\\/]+/, "")}`;
  if (p.startsWith(`${normalized}\\`)) return `~\\${p.slice(normalized.length + 1).replace(/^[\\/]+/, "")}`;
  return p;
}
