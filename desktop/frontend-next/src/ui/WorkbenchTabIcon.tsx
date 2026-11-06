import { AgentAvatar } from "./cards/SubagentGroup";
import { fileKind, langOf } from "./fileKind";
import { StudioIcon } from "./StudioIcon";
import type { Surface } from "./workbench_tabs";

const ICONS = { file: "file", job: "play", changes: "branch", terminal: "terminal" } as const;

export function WorkbenchTabIcon({ surface }: { surface: Surface }) {
  if (surface.kind === "agent") return <AgentAvatar name={surface.name} />;
  const file = surface.kind === "file";
  return <StudioIcon name={ICONS[surface.kind as keyof typeof ICONS] ?? "globe"} data-kind={file ? fileKind(surface.path) || undefined : undefined} data-lang={file ? langOf(surface.path) : undefined} />;
}
