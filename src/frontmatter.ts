/** Tiny YAML-frontmatter parser shared by skills / memory loaders. Single source so BOM + folded + quoted handling stay consistent. */

const KEY_RE = /^([a-zA-Z_][a-zA-Z0-9_-]*):\s*(.*)$/;
/** Bracket-write guard — regex permits these as identifiers, but writing them would mutate Object.prototype. */
const FORBIDDEN_KEYS = new Set(["__proto__", "constructor", "prototype"]);

function stripQuotes(s: string): string {
  if (s.length < 2) return s;
  const first = s[0];
  const last = s[s.length - 1];
  if ((first === '"' && last === '"') || (first === "'" && last === "'")) {
    return s.slice(1, -1);
  }
  return s;
}

export function parseFrontmatter(raw: string): { data: Record<string, string>; body: string } {
  const stripped = raw.charCodeAt(0) === 0xfeff ? raw.slice(1) : raw;
  const lines = stripped.split(/\r?\n/);
  if (lines[0] !== "---") return { data: {}, body: stripped };
  const end = lines.indexOf("---", 1);
  if (end < 0) return { data: {}, body: stripped };
  const entries = new Map<string, string>();
  let currentKey: string | null = null;
  for (let i = 1; i < end; i++) {
    const line = lines[i] ?? "";
    if (line.trim() === "") {
      currentKey = null;
      continue;
    }
    const m = line.match(KEY_RE);
    if (m?.[1] && !FORBIDDEN_KEYS.has(m[1])) {
      currentKey = m[1];
      entries.set(currentKey, (m[2] ?? "").trim());
    } else if (currentKey) {
      const cont = line.trim();
      const prev = entries.get(currentKey) ?? "";
      entries.set(currentKey, prev ? `${prev} ${cont}` : cont);
    }
  }
  const data: Record<string, string> = Object.create(null);
  for (const [k, v] of entries) {
    if (FORBIDDEN_KEYS.has(k)) continue;
    data[k] = stripQuotes(v);
  }
  return {
    data,
    body: lines
      .slice(end + 1)
      .join("\n")
      .replace(/^\n+/, ""),
  };
}
