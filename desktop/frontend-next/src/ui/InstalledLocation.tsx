import { useEffect, type Dispatch, type RefObject, type SetStateAction } from "react";

// The market hands over an applied action. Wait for the installed list to
// refresh before focusing its row; a tab switch can render before that read.
export function InstalledLocation({ root, target, packages, skills, mcp, refreshing, onFound }: {
  root: RefObject<HTMLDivElement | null>;
  target: { kind: string; name: string } | null;
  packages: readonly unknown[];
  skills: readonly unknown[];
  mcp: readonly unknown[];
  refreshing: boolean;
  onFound: Dispatch<SetStateAction<{ kind: string; name: string } | null>>;
}) {
  useEffect(() => {
    if (!target || refreshing) return;
    const group = root.current?.querySelector<HTMLElement>(target.kind === "skill" ? "#set-skills" : target.kind === "mcp" ? "#set-mcp" : "#set-plugins");
    group?.scrollIntoView?.({ block: "start" });
    const action = target.kind === "skill" ? "skill.enabled" : "mcp.enabled";
    const row = target.kind === "plugin" || target.kind === "theme"
      ? [...(group?.querySelectorAll<HTMLDetailsElement>("[data-extension-name]") ?? [])].find((el) => el.dataset.extensionName === target.name)?.querySelector<HTMLElement>("summary")
      : [...(group?.querySelectorAll<HTMLElement>("[data-action]") ?? [])].find((el) => el.dataset.action === action && el.dataset.target === target.name);
    if (row) {
      row.focus();
      onFound(null);
    } else {
      root.current?.querySelector<HTMLElement>("[data-action='extensions.tab'][data-value='installed']")?.focus();
      onFound(null);
    }
  }, [root, target, packages, skills, mcp, refreshing, onFound]);
  return null;
}
