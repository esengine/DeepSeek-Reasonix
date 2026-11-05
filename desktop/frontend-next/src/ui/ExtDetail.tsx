import type { ReactNode } from "react";
import { t } from "../i18n";
import type { AgentPort, McpEntry, PluginPackage, SkillEntry } from "../port/port";
import { ExtPick, type ExtPicked } from "./extpick";
import { Package } from "./Packages";
import { ServerRow } from "./ServerRow";
import { SkillRow } from "./SkillRow";

interface Props {
  picked: ExtPicked | null;
  packages: PluginPackage[];
  mcp: McpEntry[];
  skills: SkillEntry[];
  port: AgentPort;
  root: string;
  live: boolean;
  implicit: boolean;
  updating: string;
  onChanged: () => void;
  onReloadError: (message: string) => void;
  onReloaded: () => void;
  onUpdate: (name: string) => void;
  onFailed: (why: string) => void;
}

/** What the extensions page shows beside its list: the chosen package, service
 *  or skill drawn whole, with every action it offers. */
export function ExtDetail({ picked, packages, mcp, skills, port, root, live, implicit, updating, onChanged, onReloadError, onReloaded, onUpdate, onFailed }: Props) {
  const p = picked?.kind === "package" ? packages.find((x) => x.name === picked.name) : undefined;
  const m = picked?.kind === "server" ? mcp.find((x) => x.name === picked.name) : undefined;
  const sk = picked?.kind === "skill" ? skills.find((x) => x.name === picked.name) : undefined;
  let body: ReactNode = <p className="ext-detail-none">{t("ext.pick_one_to_see_it")}</p>;
  if (p) body = <Package key={p.name} p={p} hidden={false} port={port} onDone={onChanged} onReloadError={onReloadError} onReloaded={onReloaded} updating={updating} onUpdate={() => onUpdate(p.name)} detail />;
  else if (m) body = <ServerRow key={m.name} m={m} port={port} onDone={onChanged} root={root} live={live} detail />;
  else if (sk) body = <SkillRow key={sk.name} sk={sk} implicit={implicit} port={port} onDone={onChanged} root={root} onFailed={onFailed} detail />;
  return (
    <aside className="ext-detail-pane" aria-label={t("ext.details")}>
      {body}
      <p className="ext-detail-foot">{t("ext.changes_take_effect_next_turn")}</p>
    </aside>
  );
}

/** What the page shows before anything is chosen: the first thing the list holds. */
export function firstPick(packages: PluginPackage[], mcp: McpEntry[], skills: SkillEntry[]): ExtPicked | null {
  if (packages[0]) return { kind: "package", name: packages[0].name };
  if (mcp[0]) return { kind: "server", name: mcp[0].name };
  return skills[0] ? { kind: "skill", name: skills[0].name } : null;
}

/** The installed list beside its detail pane on the extensions page; anywhere
 *  else the list alone, unfolding in place. */
export function ExtSplit({ on, picked, onPick, detail, children }: { on: boolean; picked: ExtPicked | null; onPick: (p: ExtPicked) => void; detail: ReactNode; children: ReactNode }) {
  if (!on) return <>{children}</>;
  return (
    <ExtPick.Provider value={{ picked, pick: onPick }}>
      <div className="ext-md">
        <div className="ext-md-list">{children}</div>
        {detail}
      </div>
    </ExtPick.Provider>
  );
}
