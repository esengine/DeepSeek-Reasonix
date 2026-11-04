import { t } from "../i18n";
import { CopyButton } from "./CopyButton";
import { StudioIcon } from "./StudioIcon";

export function compactWorkspaceValue(value: string, length = 38): string {
  const chars = Array.from(value);
  if (chars.length <= length) return value;
  const tail = Math.min(16, Math.floor(length / 2));
  return chars.slice(0, length - tail - 1).join("") + "…" + chars.slice(-tail).join("");
}

export function workspaceName(path: string): string {
  return path.split(/[\\/]/).filter(Boolean).at(-1) || path;
}

export function WorkspaceBranchName({ value }: { value: string }) {
  const chars = Array.from(value);
  return <span className="workspace-branch-value" title={value}>{chars.length > 32 ? <><span>{chars.slice(0, -12).join("")}</span><span>{chars.slice(-12).join("")}</span></> : value}</span>;
}

export function WorkspacePath({ path, onResize }: { path: string; onResize?: () => void }) {
  const parts = path.split(/[\\/]/).filter(Boolean);
  const short = parts.length > 2 ? "…/" + parts.slice(-2).join("/") : path;
  return <div className="workspace-path">
    <details data-action="workspace.configure" onToggle={onResize}>
      <summary aria-label={t("session.show_full_path")} title={path}><StudioIcon name="folder"/><span>{short}</span><StudioIcon name="down"/></summary>
      <code>{path}</code>
    </details>
    <CopyButton text={path} iconOnly className="workspace-path-copy" label={t("session.copy_full_path")}/>
  </div>;
}
