import type { ReactNode } from "react";
import type { AgentPort, ContextBreakdown, McpEntry, SessionStatus } from "../port/port";
import type { RuntimeView } from "../port/hub";

// PaneReport is what the window's own chrome needs from whichever pane has
// focus: everything else about a session stays inside the pane that owns it.
export interface PaneReport {
  status: SessionStatus | null;
  title: string;
  steer: number;
  run: string;
  // Whether the turn is actually moving or waiting on you. "halt" cannot answer
  // this: a reopened history sits at halt too, and closing that costs nothing.
  live: boolean;
  cost: string;
  contextPercent: number | null;
  context: ContextBreakdown | null;
  mcp: McpEntry[];
  wallet: string;
}

export interface PaneProps {
  port: AgentPort;
  rt: RuntimeView;
  title: string;
  active: boolean;
  // Where the metrics rail lives. Only the focused pane renders into it, so the
  // column stays on the window's edge instead of appearing between two panes.
  sideHost: HTMLElement | null;
  side: boolean;
  onFocus: () => void;
  // Every pane reports, not just the focused one: a tab has to show that the
  // conversation behind it is still working.
  onReport: (id: string, report: PaneReport) => void;
  // Off-screen panes stay mounted — their stream, transcript and scroll
  // position are exactly what a tab switch must not throw away.
  visible: boolean;
  onSessionChanged: () => void;
  onTurnDone?: (id: string) => void;
  // Bumped when something outside this pane changed a setting that belongs to
  // its session. /status is polled only while a turn runs, so without this the
  // pane keeps reporting the posture it had when it opened.
  pulse: number;
  findPulse: number;
  onSettings: (section?: string) => void;
  // 这个窗口还没有人选过的项目文件夹。空转录是唯一说得出这句话的地方 —— 那里
  // 本来就在替一段还没开始的对话说明它该怎么开始。
  needsProject: boolean;
  onOpenProject: () => void;
  onKeepHere: () => void;
  // A prop, not a document read: this pane is memoised past an attribute flip.
  theme: string;
  // Rides on `.app`: that is where the divider writes while a drag is in flight.
  dockW: number;
  dockMax: number;
  onDockW: (w: number) => void;
  manualBrowser?: boolean;
  onManualBrowser?: (on: boolean) => void;
  // The window's failed-request notice, handed only to the pane in front.
  alert?: ReactNode;
}
