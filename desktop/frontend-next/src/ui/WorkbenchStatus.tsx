import { t } from "../i18n";

const LANGUAGE: Record<string, string> = {
  go: "Go", py: "Python", ts: "TypeScript", tsx: "TypeScript", js: "JavaScript", jsx: "JavaScript", rs: "Rust",
  java: "Java", kt: "Kotlin", c: "C", h: "C", cpp: "C++", cs: "C#", rb: "Ruby", php: "PHP", swift: "Swift",
  md: "Markdown", json: "JSON", yaml: "YAML", yml: "YAML", toml: "TOML", css: "CSS", html: "HTML", sh: "Shell", sql: "SQL",
};

/** The open file's language, the cursor's line and whether there is unsaved text. */
export function WorkbenchStatus({ path, line, dirty }: { path: string; line: number; dirty: boolean }) {
  const ext = path.slice(path.lastIndexOf(".") + 1).toLowerCase();
  return (
    <div className="workbench-status">
      <span>{LANGUAGE[ext] ?? ext.toUpperCase()}</span>
      <span className="workbench-status-fill" />
      <span>{t("workbench.line_n", { n: line })}</span>
      {dirty && <span className="workbench-status-dirty">{t("workbench.unsaved")}</span>}
    </div>
  );
}
