import { useState, type ReactNode } from "react";
import { Button, FocusTrap, Group as MantineGroup, Paper, Stack, Text, Title } from "@mantine/core";
import { t } from "../../../i18n";
import type { ExtensionViewNode } from "../../../port/wire";
import "./rich.css";

export interface RichInputProps {
  node: ExtensionViewNode;
  value: unknown;
  disabled: boolean;
  change: (value: string | string[]) => void;
}

export function Workspace({ title, subtitle, badge, children }: { title?: string; subtitle: string; badge: string; children: ReactNode }) {
  const [expanded, setExpanded] = useState(false);
  return <FocusTrap active={expanded}><section className={`rich-workspace${expanded ? " rich-expanded" : ""}`} role={expanded ? "dialog" : "region"} aria-modal={expanded || undefined} aria-label={title || badge} tabIndex={-1} data-action="surface.explore" onKeyDown={e => { if (e.key === "Escape" && expanded) { e.stopPropagation(); setExpanded(false); } }}>
    <Paper className="rich-workspace-body" radius="lg"><Stack gap="lg">
      <MantineGroup className="rich-heading" justify="space-between" align="flex-start" wrap="nowrap">
        <Stack gap={6}><Text className="rich-eyebrow" size="xs" fw={600} c="dimmed">{badge}</Text><Title order={3} className="rich-title">{title}</Title><Text c="dimmed" size="sm">{subtitle}</Text></Stack>
        <Button variant="subtle" size="xs" data-action="surface.explore" onClick={() => setExpanded(v => !v)}>{expanded ? t("chat.collapse_workspace") : t("chat.expand_workspace")}</Button>
      </MantineGroup>
      {children}
    </Stack></Paper>
  </section></FocusTrap>;
}
