import { memo, useCallback, useState, type ReactNode } from "react";
import type { AgentPort, ContextBreakdown, JobEntry, McpEntry, WorkspaceChanges } from "../port/port";
import type { ExtensionSurface } from "../port/wire";
import type { Metrics as M, PlanStep } from "../state/session";
import { Agents } from "./panels/Agents";
import { Cache } from "./panels/Cache";
import { Context } from "./panels/Context";
import { Jobs } from "./panels/Jobs";
import { Files } from "./panels/Files";
import { Mcp } from "./panels/Mcp";
import { Extensions } from "./panels/Extensions";
import { Runtime } from "./panels/Runtime";
import { Cost } from "./panels/Cost";
import { Plan } from "./Plan";
import type { Rail } from "./panels/derive";
import type { Wallet } from "./wallet";
import { swapping } from "./swap";
import { ChangePreview } from "./ChangePreview";
import type { Posture } from "./decisions";

// What the rail can show. Presence is still each panel's own contract — a
// panel with nothing to say draws nothing — so this decides where something
// goes, never whether there is something.
type Section = "cache" | "cost" | "context" | "agents" | "runtime" | "plan" | "files" | "jobs" | "mcp" | "extensions";

// Fixed per posture, and read off neither the panels' contents nor the size of
// anything: an order that follows the data puts a panel somewhere new the
// moment it has something, which is the instability this replaces.
//
// Working answers "is it alive, what is it doing, how much room is left, where
// is it in the plan". Review answers "what changed, what came of it, what did
// it cost" — and context drops, because how full the window is matters while
// there is a turn to fit and is forensic once there is not.
const ORDER: Record<Posture, readonly Section[]> = {
  working: ["context", "runtime", "agents", "plan", "cost", "cache", "files", "jobs", "mcp", "extensions"],
  review: ["files", "plan", "jobs", "cost", "cache", "context", "runtime", "agents", "mcp", "extensions"],
};

interface Props extends Rail {
  port: AgentPort;
  metrics: M;
  jobs: JobEntry[];
  mcp: McpEntry[];
  rate: number;
  done: boolean;
  /** Which reading this rail is for. It decides order and nothing else: what
   *  each panel is, whether it appears at all, and what it reads are its own. */
  posture: Posture;
  plan: PlanStep[];
  wallet: Wallet;
  account: string;
  onRefreshWallet: () => void;
  tree: WorkspaceChanges | null;
  ctx: ContextBreakdown | null;
  // Declaring the missing window rebuilds the runtime, so the rebuilt gauge
  // comes back on that same call rather than on the next poll.
  onCtx: (next: ContextBreakdown) => void;
  yolo: boolean;
  /** The task view has the plan at full width, so the rail's copy stands down. */
  planShownElsewhere?: boolean;
  onSettings: () => void;
  panels: ExtensionSurface[];
  // Composed views that resolved to the rail — the default place for a standing
  // surface nobody assigned elsewhere.
  views?: ExtensionSurface[];
  onExtInvoke: (name: string) => void;
  onMoveSurface?: (ext: ExtensionSurface, slot: string) => void;
}

// The inspector. Most of these speak only when they have something to report,
// which is what keeps it from being a permanent nine-panel wall a glance has
// to skip six of; the order they speak in is the table above. The walk behind
// all of it happens once, in the pane, and arrives here derived.
//
// There is no head card, and nothing here describes one any more: Context's
// row=true form for it is gone with the rest. The figures here are the only
// ones there are.
export const Metrics = memo(function Metrics({
  port,
  metrics,
  tasks,
  changes,
  stats,
  jobs,
  mcp,
  rate,
  done,
  posture,
  plan,
  wallet,
  account,
  onRefreshWallet,
  tree,
  ctx,
  onCtx,
  yolo,
  planShownElsewhere,
  onSettings,
  panels,
  views,
  onExtInvoke,
  onMoveSurface,
}: Props) {
  // The rail owns which change is open, so it closes with the pane rather than
  // outliving it in a window-level store.
  const [openPath, setOpenPath] = useState<string | null>(null);
  // Both ends go through the swap: the preview is conditionally rendered, so
  // without one it has an entrance and no way out — it stops existing, which
  // reads as the window flinching rather than as the panel leaving.
  const openPreview = useCallback((path: string) => swapping(() => setOpenPath(path), "cpv"), []);
  const closePreview = useCallback(() => swapping(() => setOpenPath(null), "cpv"), []);
  // One list of panels, built once, ordered by the table below. Two branches
  // of JSX would be the obvious way to reorder and the wrong one: the same
  // panel under a different parent is a different element to React, so it
  // would be torn down and rebuilt — losing whatever it had open, and asking
  // again for whatever it reads on mount. Changing which posture the rail is
  // in is not a change to any panel's lifetime.
  const panel: Record<Section, ReactNode> = {
    cache: <Cache key="cache" metrics={metrics} />,
    cost: <Cost key="cost" metrics={metrics} wallet={wallet} account={account} onRefreshWallet={onRefreshWallet} />,
    context: <Context key="context" ctx={ctx} legend port={port} onCtx={onCtx} />,
    agents: <Agents key="agents" tasks={tasks} />,
    runtime: <Runtime key="runtime" rate={rate} done={done} stats={stats} />,
    plan: <Plan key="plan" steps={plan} shownElsewhere={planShownElsewhere} />,
    files: (
      <Files key="files" changes={changes} yolo={yolo} tree={tree} open={openPath}
        onOpen={tree?.repo ? openPreview : undefined} />
    ),
    jobs: <Jobs key="jobs" jobs={jobs} />,
    // Not folded behind a diagnostics drawer, which is where this was headed.
    // Mcp already speaks only when a server has failed or has never been
    // answered for, so the drawer would be empty on every rail that is fine
    // and closed over the one thing worth seeing on a rail that is not.
    mcp: <Mcp key="mcp" servers={mcp} onOpen={onSettings} />,
    extensions: (
      <Extensions key="extensions" panels={panels} views={views} onInvoke={onExtInvoke} onMove={onMoveSurface} />
    ),
  };
  return (
    <>
      <div className="scroll">
        {ORDER[posture].map((id) => panel[id])}
      </div>
      {openPath && <ChangePreview port={port} path={openPath} onClose={closePreview} />}
    </>
  );
});
