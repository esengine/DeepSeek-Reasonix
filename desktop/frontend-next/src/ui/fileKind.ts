const KINDS: Record<string, string[]> = {
  code: ["ts", "tsx", "js", "jsx", "mjs", "cjs", "go", "py", "rs", "java", "kt", "swift", "c", "h", "cc", "cpp", "hpp", "cs", "rb", "php", "sh", "zsh", "bash", "ps1", "lua", "sql", "css", "scss", "html", "vue", "svelte"],
  doc: ["md", "mdx", "txt", "rst", "pdf", "doc", "docx"],
  data: ["json", "jsonl", "yaml", "yml", "toml", "ini", "csv", "tsv", "xml", "lock", "env"],
  image: ["png", "jpg", "jpeg", "gif", "webp", "svg", "ico", "bmp", "avif"],
};

const BY_EXT = new Map(Object.entries(KINDS).flatMap(([kind, exts]) => exts.map((ext) => [ext, kind] as const)));

export function fileKind(path: string): string {
  const name = path.slice(path.lastIndexOf("/") + 1);
  const dot = name.lastIndexOf(".");
  return dot < 0 ? "" : (BY_EXT.get(name.slice(dot + 1).toLowerCase()) ?? "");
}

const LANG: Record<string, string> = { go: "go", py: "python", xlsx: "sheet", xls: "sheet", csv: "sheet", tsv: "sheet" };

/** The few languages the workbench's tabs colour apart — Go, Python, sheets — else none. */
export function langOf(path: string): string | undefined {
  const name = path.slice(path.lastIndexOf("/") + 1);
  const dot = name.lastIndexOf(".");
  return dot < 0 ? undefined : LANG[name.slice(dot + 1).toLowerCase()];
}
