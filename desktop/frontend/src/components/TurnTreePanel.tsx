import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties, type PointerEvent } from "react";
import { ChevronDown, ChevronRight, GitBranch, LocateFixed, RefreshCw, X, ZoomIn, ZoomOut } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { TurnNodeInfo, TurnTreeData } from "../lib/types";
import {
  readTurnTreeLayoutPreference,
  TURN_TREE_LAYOUT_EVENT,
  type TurnTreeLayoutMode,
} from "../lib/turnTreeLayout";
import { ResizableDrawer } from "./ResizableDrawer";
import { Tooltip } from "./Tooltip";

const NODE_W = 248;
const NODE_H = 74;
const TREE_W = 390;
const DEPTH_W = 86;
const LANE_W = 312;
const ROOT_GAP = 96;
const METRO_GROUP_W = 560;
const METRO_CARD_X = 136;
const METRO_RAIL_X = 48;
const METRO_RAIL_STEP = 18;
const ROW_H = 112;
const PAD_X = 32;
const PAD_Y = 28;
const ACTION_W = 360;
const ACTION_H = 188;
const ACTION_MARGIN = 12;

interface PositionedNode extends TurnNodeInfo {
  x: number;
  y: number;
  row: number;
  rootIndex: number;
  lane: number;
  railX: number;
  cardWidth: number;
}

interface LayoutLine {
  key: string;
  x1: number;
  y1: number;
  x2: number;
  y2: number;
  lane: number;
}

interface LayoutResult {
  nodes: PositionedNode[];
  width: number;
  height: number;
  lines: LayoutLine[];
}

interface TurnTreeNodeTarget {
  tabId: string;
  rollback?: () => Promise<void> | void;
}

const lineColors = ["#3b82f6", "#f59e0b", "#10b981", "#ec4899", "#8b5cf6", "#ef4444", "#14b8a6", "#64748b"];

function lineColor(lane: number): string {
  return lineColors[Math.abs(lane) % lineColors.length];
}

function nodeKey(node: TurnNodeInfo): string {
  return node.key || `${node.branchId}:${node.turn}`;
}

function shortBranch(id: string): string {
  if (id.length <= 18) return id;
  return `${id.slice(0, 8)}...${id.slice(-6)}`;
}

function compactNumber(value: number): string {
  if (!Number.isFinite(value)) return "0";
  return new Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 }).format(Math.max(0, value));
}

function visibleNodes(nodes: TurnNodeInfo[], collapsed: Set<string>): TurnNodeInfo[] {
  const out: TurnNodeInfo[] = [];
  const hiddenDepths: number[] = [];
  for (const node of nodes) {
    while (hiddenDepths.length > 0 && node.depth <= hiddenDepths[hiddenDepths.length - 1]) hiddenDepths.pop();
    if (hiddenDepths.length === 0) out.push(node);
    if (node.hasFork && collapsed.has(nodeKey(node))) hiddenDepths.push(node.depth);
  }
  return out;
}

function treeConnector(parent: PositionedNode, child: PositionedNode): string {
  if (parent.x === child.x) {
    const x = parent.x + NODE_W / 2;
    return `M ${x} ${parent.y + NODE_H} L ${x} ${child.y}`;
  }
  const x1 = parent.x + NODE_W / 2;
  const y1 = parent.y + NODE_H;
  const x2 = child.x;
  const y2 = child.y + NODE_H / 2;
  const elbowY = y1 + Math.max(18, (y2 - y1) / 2);
  return `M ${x1} ${y1} L ${x1} ${elbowY} C ${x1} ${y2}, ${x2 - 28} ${y2}, ${x2} ${y2}`;
}

function laneConnector(parent: PositionedNode, child: PositionedNode): string {
  if (parent.x === child.x) {
    const x = parent.x + NODE_W / 2;
    return `M ${x} ${parent.y + NODE_H} L ${x} ${child.y}`;
  }
  const x1 = parent.x + NODE_W / 2;
  const y1 = parent.y + NODE_H;
  const x2 = child.x + NODE_W / 2;
  const y2 = child.y;
  const elbowY = y1 + Math.max(18, (y2 - y1) / 2);
  return `M ${x1} ${y1} L ${x1} ${elbowY} C ${x1} ${y2}, ${x2} ${elbowY}, ${x2} ${y2}`;
}

function metroConnector(parent: PositionedNode, child: PositionedNode): string {
  const x1 = parent.railX;
  const y1 = parent.y + NODE_H / 2;
  const x2 = child.railX;
  const y2 = child.y + NODE_H / 2;
  if (x1 === x2) return `M ${x1} ${y1} L ${x2} ${y2}`;
  const elbowY = y1 + Math.max(18, (y2 - y1) / 2);
  return `M ${x1} ${y1} L ${x1} ${elbowY} C ${x1} ${y2}, ${x2} ${elbowY}, ${x2} ${y2}`;
}

function treeLayout(nodes: TurnNodeInfo[]): LayoutResult {
  const rowsByRoot = new Map<number, number>();
  const positioned = nodes.map((node) => {
    const rootIndex = Number.isFinite(node.rootIndex) ? node.rootIndex : 0;
    const row = rowsByRoot.get(rootIndex) ?? 0;
    rowsByRoot.set(rootIndex, row + 1);
    return {
      ...node,
      rootIndex,
      row,
      lane: node.depth,
      railX: 0,
      cardWidth: NODE_W,
      x: PAD_X + rootIndex * TREE_W + node.depth * DEPTH_W,
      y: PAD_Y + row * ROW_H,
    };
  });
  const rootCount = Math.max(1, ...positioned.map((node) => node.rootIndex + 1));
  const maxRows = Math.max(1, ...Array.from({ length: rootCount }, (_, rootIndex) => positioned.filter((node) => node.rootIndex === rootIndex).length));
  return { nodes: positioned, lines: [], width: Math.max(720, PAD_X * 2 + rootCount * TREE_W), height: Math.max(360, PAD_Y * 2 + maxRows * ROW_H) };
}

function assignBranchLanes(nodes: TurnNodeInfo[]) {
  const lanesByBranch = new Map<string, number>();
  const nextLaneByRoot = new Map<number, number>();
  const maxLaneByRoot = new Map<number, number>();
  const laneByKey = new Map<string, number>();
  for (const node of nodes) {
    const rootIndex = Number.isFinite(node.rootIndex) ? node.rootIndex : 0;
    let lane = lanesByBranch.get(node.branchId);
    if (lane === undefined) {
      lane = node.depth === 0 ? 0 : (nextLaneByRoot.get(rootIndex) ?? 1);
      lanesByBranch.set(node.branchId, lane);
      nextLaneByRoot.set(rootIndex, Math.max(nextLaneByRoot.get(rootIndex) ?? 1, lane + 1));
    }
    laneByKey.set(nodeKey(node), lane);
    maxLaneByRoot.set(rootIndex, Math.max(maxLaneByRoot.get(rootIndex) ?? 0, lane));
  }
  return { laneByKey, maxLaneByRoot };
}

function rootOffsets(maxLaneByRoot: Map<number, number>, rootCount: number) {
  const offsets = new Map<number, number>();
  let x = PAD_X;
  for (let rootIndex = 0; rootIndex < rootCount; rootIndex += 1) {
    offsets.set(rootIndex, x);
    x += ((maxLaneByRoot.get(rootIndex) ?? 0) + 1) * LANE_W + ROOT_GAP;
  }
  return { offsets, width: x + PAD_X - ROOT_GAP };
}

function laneLayout(nodes: TurnNodeInfo[]): LayoutResult {
  const rootCount = Math.max(1, ...nodes.map((node) => node.rootIndex + 1));
  const { laneByKey, maxLaneByRoot } = assignBranchLanes(nodes);
  const { offsets, width } = rootOffsets(maxLaneByRoot, rootCount);
  const maxTurn = Math.max(0, ...nodes.map((node) => node.turn));
  const positioned = nodes.map((node) => {
    const rootIndex = Number.isFinite(node.rootIndex) ? node.rootIndex : 0;
    const lane = laneByKey.get(nodeKey(node)) ?? 0;
    return {
      ...node,
      rootIndex,
      lane,
      row: node.turn,
      railX: 0,
      cardWidth: NODE_W,
      x: (offsets.get(rootIndex) ?? PAD_X) + lane * LANE_W,
      y: PAD_Y + node.turn * ROW_H,
    };
  });
  const lines = Array.from(new Map(positioned.map((node) => [`${node.rootIndex}:${node.lane}`, node])).values()).map((node) => ({
    key: `lane:${node.rootIndex}:${node.lane}`,
    x1: node.x + NODE_W / 2,
    y1: PAD_Y,
    x2: node.x + NODE_W / 2,
    y2: Math.max(360, PAD_Y * 2 + (maxTurn + 1) * ROW_H) - PAD_Y,
    lane: node.lane,
  }));
  return { nodes: positioned, lines, width: Math.max(720, width + NODE_W), height: Math.max(360, PAD_Y * 2 + (maxTurn + 1) * ROW_H) };
}

function metroLayout(nodes: TurnNodeInfo[]): LayoutResult {
  const { laneByKey, maxLaneByRoot } = assignBranchLanes(nodes);
  const rowsByRoot = new Map<number, number>();
  const positioned = nodes.map((node) => {
    const rootIndex = Number.isFinite(node.rootIndex) ? node.rootIndex : 0;
    const row = rowsByRoot.get(rootIndex) ?? 0;
    rowsByRoot.set(rootIndex, row + 1);
    const lane = laneByKey.get(nodeKey(node)) ?? 0;
    const groupX = PAD_X + rootIndex * METRO_GROUP_W;
    return {
      ...node,
      rootIndex,
      row,
      lane,
      railX: groupX + METRO_RAIL_X + Math.min(lane, 7) * METRO_RAIL_STEP,
      cardWidth: 344,
      x: groupX + METRO_CARD_X,
      y: PAD_Y + row * ROW_H,
    };
  });
  const rootCount = Math.max(1, ...positioned.map((node) => node.rootIndex + 1));
  const maxRows = Math.max(1, ...Array.from({ length: rootCount }, (_, rootIndex) => positioned.filter((node) => node.rootIndex === rootIndex).length));
  const height = Math.max(360, PAD_Y * 2 + maxRows * ROW_H);
  const lines: LayoutLine[] = [];
  for (let rootIndex = 0; rootIndex < rootCount; rootIndex += 1) {
    const groupX = PAD_X + rootIndex * METRO_GROUP_W;
    const maxLane = maxLaneByRoot.get(rootIndex) ?? 0;
    for (let lane = 0; lane <= maxLane; lane += 1) {
      const x = groupX + METRO_RAIL_X + Math.min(lane, 7) * METRO_RAIL_STEP;
      lines.push({ key: `metro:${rootIndex}:${lane}`, x1: x, y1: PAD_Y, x2: x, y2: height - PAD_Y, lane });
    }
  }
  return { nodes: positioned, lines, width: Math.max(720, PAD_X * 2 + rootCount * METRO_GROUP_W), height };
}

function buildLayout(mode: TurnTreeLayoutMode, nodes: TurnNodeInfo[]): LayoutResult {
  if (mode === "lanes") return laneLayout(nodes);
  if (mode === "metro") return metroLayout(nodes);
  return treeLayout(nodes);
}

function connectorFor(mode: TurnTreeLayoutMode, parent: PositionedNode, child: PositionedNode): string {
  if (mode === "lanes") return laneConnector(parent, child);
  if (mode === "metro") return metroConnector(parent, child);
  return treeConnector(parent, child);
}

export function TurnTreePanel({
  tabId,
  running,
  onClose,
  onJumped,
  resolveNodeTab,
}: {
  tabId: string;
  running: boolean;
  onClose: () => void;
  onJumped: (tabId: string) => Promise<void> | void;
  resolveNodeTab: (node: TurnNodeInfo, mode: "current" | "new") => Promise<TurnTreeNodeTarget | undefined>;
}) {
  const t = useT();
  const [tree, setTree] = useState<TurnTreeData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const [selectedKey, setSelectedKey] = useState("");
  const [pendingNode, setPendingNode] = useState<TurnNodeInfo | null>(null);
  const [actionPosition, setActionPosition] = useState({ x: ACTION_MARGIN, y: ACTION_MARGIN });
  const [zoom, setZoom] = useState(1);
  const [dragging, setDragging] = useState(false);
  const [layoutMode, setLayoutMode] = useState<TurnTreeLayoutMode>(() => readTurnTreeLayoutPreference());
  const currentRef = useRef<HTMLDivElement | null>(null);
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const dragRef = useRef({ x: 0, y: 0, left: 0, top: 0 });
  const actionDragRef = useRef({ x: 0, y: 0, left: 0, top: 0 });

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const data = await app.TurnTreeForTab(tabId);
      setTree(data);
      setSelectedKey(data.currentKey);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [tabId]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    const onLayout = (event: Event) => {
      const next = (event as CustomEvent<TurnTreeLayoutMode>).detail;
      setLayoutMode(next);
    };
    window.addEventListener(TURN_TREE_LAYOUT_EVENT, onLayout);
    return () => window.removeEventListener(TURN_TREE_LAYOUT_EVENT, onLayout);
  }, []);

  const layout = useMemo(() => buildLayout(layoutMode, visibleNodes(tree?.nodes ?? [], collapsed)), [collapsed, layoutMode, tree]);
  const positioned = layout.nodes;
  const byKey = useMemo(() => new Map(positioned.map((node) => [nodeKey(node), node])), [positioned]);
  const edges = useMemo(() => {
    const out: Array<[PositionedNode, PositionedNode]> = [];
    for (const child of positioned) {
      const parent = child.parentKey ? byKey.get(child.parentKey) : undefined;
      if (parent) out.push([parent, child]);
    }
    return out;
  }, [byKey, positioned]);
  const selected = selectedKey ? byKey.get(selectedKey) : undefined;

  const jump = useCallback(async (node: TurnNodeInfo, mode: "current" | "new") => {
    if (running) return;
    if (node.isCurrent && mode === "current") {
      setPendingNode(null);
      return;
    }
    setSelectedKey(nodeKey(node));
    setPendingNode(null);
    setError("");
    try {
      const target = await resolveNodeTab(node, mode);
      const targetTabId = target?.tabId || tabId;
      try {
        await app.JumpToTurnForTab(targetTabId, node.branchId, node.turn);
        if (mode === "new") await app.PersistTurnPreviewForTab(targetTabId);
      } catch (jumpError) {
        await target?.rollback?.();
        throw jumpError;
      }
      await onJumped(targetTabId);
      const data = await app.TurnTreeForTab(targetTabId);
      setTree(data);
      setSelectedKey(data.currentKey);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, [onJumped, resolveNodeTab, running, tabId]);

  const clampActionPosition = useCallback((x: number, y: number) => {
    const scroller = scrollRef.current;
    if (!scroller) return { x, y };
    const minX = scroller.scrollLeft + ACTION_MARGIN;
    const minY = scroller.scrollTop + ACTION_MARGIN;
    const maxX = scroller.scrollLeft + Math.max(ACTION_MARGIN, scroller.clientWidth - ACTION_W - ACTION_MARGIN);
    const maxY = scroller.scrollTop + Math.max(ACTION_MARGIN, scroller.clientHeight - ACTION_H - ACTION_MARGIN);
    return {
      x: Math.min(maxX, Math.max(minX, x)),
      y: Math.min(maxY, Math.max(minY, y)),
    };
  }, []);

  const requestJump = useCallback((node: PositionedNode) => {
    if (running) return;
    setSelectedKey(nodeKey(node));
    setPendingNode(node);
    const rightSideX = (node.x + node.cardWidth) * zoom + ACTION_MARGIN;
    const leftSideX = node.x * zoom - ACTION_W - ACTION_MARGIN;
    const scroller = scrollRef.current;
    const visibleRight = scroller ? scroller.scrollLeft + scroller.clientWidth : Number.POSITIVE_INFINITY;
    const preferredX = rightSideX + ACTION_W <= visibleRight - ACTION_MARGIN ? rightSideX : leftSideX;
    setActionPosition(clampActionPosition(preferredX, node.y * zoom));
  }, [clampActionPosition, running, zoom]);

  const focusCurrent = useCallback(() => {
    currentRef.current?.scrollIntoView({ behavior: "smooth", block: "center", inline: "center" });
  }, []);

  useEffect(() => {
    if (!loading) window.setTimeout(focusCurrent, 50);
  }, [focusCurrent, loading, layoutMode]);

  const startPan = useCallback((event: PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) return;
    const target = event.target as HTMLElement | null;
    if (target?.closest(".turn-tree-card, .turn-tree-action, button, a, input, textarea, select")) return;
    const scroller = scrollRef.current;
    if (!scroller) return;
    dragRef.current = { x: event.clientX, y: event.clientY, left: scroller.scrollLeft, top: scroller.scrollTop };
    setDragging(true);
    scroller.setPointerCapture(event.pointerId);
    event.preventDefault();
  }, []);

  const pan = useCallback((event: PointerEvent<HTMLDivElement>) => {
    if (!dragging) return;
    const scroller = scrollRef.current;
    if (!scroller) return;
    const drag = dragRef.current;
    scroller.scrollLeft = drag.left - (event.clientX - drag.x);
    scroller.scrollTop = drag.top - (event.clientY - drag.y);
  }, [dragging]);

  const stopPan = useCallback((event: PointerEvent<HTMLDivElement>) => {
    if (!dragging) return;
    scrollRef.current?.releasePointerCapture(event.pointerId);
    setDragging(false);
  }, [dragging]);

  const startActionDrag = useCallback((event: PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) return;
    actionDragRef.current = { x: event.clientX, y: event.clientY, left: actionPosition.x, top: actionPosition.y };
    event.currentTarget.setPointerCapture(event.pointerId);
    event.preventDefault();
  }, [actionPosition]);

  const dragAction = useCallback((event: PointerEvent<HTMLDivElement>) => {
    if (!event.currentTarget.hasPointerCapture(event.pointerId)) return;
    const drag = actionDragRef.current;
    const next = clampActionPosition(drag.left + event.clientX - drag.x, drag.top + event.clientY - drag.y);
    setActionPosition(next);
  }, [clampActionPosition]);

  const stopActionDrag = useCallback((event: PointerEvent<HTMLDivElement>) => {
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
  }, []);

  return (
    <ResizableDrawer onClose={onClose} wide>
      <header className="drawer__head turn-tree-head">
        <div>
          <div className="drawer__title turn-tree-title">
            <GitBranch size={16} />
            {t("turnTree.title")}
          </div>
          <div className="drawer__summary">
            {selected ? t("turnTree.selected", { turn: selected.turn + 1, branch: shortBranch(selected.branchId) }) : t("turnTree.summary")}
          </div>
        </div>
        <div className="drawer__actions">
          <Tooltip label={t("turnTree.zoomOut")}>
            <button className="icon-btn" type="button" onClick={() => setZoom((value) => Math.max(0.72, value - 0.08))}>
              <ZoomOut size={14} />
            </button>
          </Tooltip>
          <Tooltip label={t("turnTree.zoomIn")}>
            <button className="icon-btn" type="button" onClick={() => setZoom((value) => Math.min(1.28, value + 0.08))}>
              <ZoomIn size={14} />
            </button>
          </Tooltip>
          <Tooltip label={t("turnTree.currentNode")}>
            <button className="icon-btn" type="button" onClick={focusCurrent}>
              <LocateFixed size={14} />
            </button>
          </Tooltip>
          <Tooltip label={t("turnTree.refresh")}>
            <button className="icon-btn" type="button" onClick={() => void load()}>
              <RefreshCw size={14} />
            </button>
          </Tooltip>
          <Tooltip label={t("common.close")}>
            <button className="icon-btn" type="button" onClick={onClose}>
              <X size={14} />
            </button>
          </Tooltip>
        </div>
      </header>
      <div className="drawer__body turn-tree-body">
        {loading ? (
          <div className="turn-tree-state">{t("turnTree.loading")}</div>
        ) : error ? (
          <div className="turn-tree-state turn-tree-state--error">
            <span>{error}</span>
            <button className="chip" type="button" onClick={() => void load()}>{t("turnTree.retry")}</button>
          </div>
        ) : positioned.length === 0 ? (
          <div className="turn-tree-state">{t("turnTree.empty")}</div>
        ) : (
          <div
            ref={scrollRef}
            className={`turn-tree-canvas-scroll${dragging ? " turn-tree-canvas-scroll--dragging" : ""}`}
            onPointerDown={startPan}
            onPointerMove={pan}
            onPointerUp={stopPan}
            onPointerCancel={stopPan}
          >
            <div className="turn-tree-canvas" style={{ width: layout.width * zoom, height: layout.height * zoom }}>
              <div style={{ transform: `scale(${zoom})`, transformOrigin: "top left", width: layout.width, height: layout.height }}>
                <svg className="turn-tree-edges" width={layout.width} height={layout.height} aria-hidden="true">
                  {layout.lines.map((line) => (
                    <line
                      key={line.key}
                      x1={line.x1}
                      y1={line.y1}
                      x2={line.x2}
                      y2={line.y2}
                      className="turn-tree-lane"
                      style={{ "--tree-line": lineColor(line.lane) } as CSSProperties}
                    />
                  ))}
                  {edges.map(([parent, child]) => (
                    <path
                      key={`${nodeKey(parent)}>${nodeKey(child)}`}
                      d={connectorFor(layoutMode, parent, child)}
                      className={`turn-tree-edge${parent.isCurrent || child.isCurrent ? " turn-tree-edge--current" : ""}`}
                      style={{ "--tree-line": lineColor(child.lane) } as CSSProperties}
                    />
                  ))}
                  {layoutMode === "metro" && positioned.map((node) => (
                    <g key={`stop:${nodeKey(node)}`} style={{ "--tree-line": lineColor(node.lane) } as CSSProperties}>
                      <line
                        x1={node.railX}
                        y1={node.y + NODE_H / 2}
                        x2={node.x}
                        y2={node.y + NODE_H / 2}
                        className="turn-tree-spoke"
                      />
                      <circle
                        cx={node.railX}
                        cy={node.y + NODE_H / 2}
                        r={node.isCurrent ? 7 : 5}
                        className={`turn-tree-stop${node.isCurrent ? " turn-tree-stop--current" : ""}`}
                      />
                    </g>
                  ))}
                </svg>
                {positioned.map((node) => {
                  const key = nodeKey(node);
                  const isSelected = key === selectedKey;
                  const cardClasses = [
                    "turn-tree-card",
                    node.isCurrent ? "turn-tree-card--current" : "",
                    isSelected ? "turn-tree-card--selected" : "",
                    node.hasFork ? "turn-tree-card--fork" : "",
                  ].filter(Boolean).join(" ");
                  const activateNode = () => {
                    requestJump(node);
                  };
                  return (
                    <div
                      key={key}
                      ref={node.isCurrent ? currentRef : undefined}
                      className={cardClasses}
                      style={{
                        left: node.x,
                        top: node.y,
                        "--tree-line": lineColor(node.lane),
                        "--turn-tree-card-width": `${node.cardWidth}px`,
                      } as CSSProperties}
                      role="button"
                      tabIndex={running ? -1 : 0}
                      aria-disabled={running || undefined}
                      onClick={activateNode}
                      onKeyDown={(event) => {
                        if (running) return;
                        if (event.key === "Enter" || event.key === " ") {
                          event.preventDefault();
                          requestJump(node);
                        }
                      }}
                    >
                      <span className="turn-tree-card__rail" />
                      <span className="turn-tree-card__top">
                        <span className="turn-tree-card__turn">T{node.turn + 1}</span>
                        <span className="turn-tree-card__branch">{node.branchName || shortBranch(node.branchId)}</span>
                        <span className="turn-tree-card__metric">
                          {t("turnTree.prefixChars", { count: compactNumber(node.prefixChars) })}
                        </span>
                        {node.hasFork && (
                          <button
                            className="turn-tree-card__fold"
                            type="button"
                            aria-expanded={!collapsed.has(key)}
                            aria-label={t("turnTree.toggleForks", { count: node.childCount })}
                            onClick={(event) => {
                              event.stopPropagation();
                              setCollapsed((prev) => {
                                const next = new Set(prev);
                                if (next.has(key)) next.delete(key);
                                else next.add(key);
                                return next;
                              });
                            }}
                          >
                            {collapsed.has(key) ? <ChevronRight size={12} /> : <ChevronDown size={12} />}
                            {node.childCount}
                          </button>
                        )}
                      </span>
                      <span className="turn-tree-card__prompt">{node.prompt || t("turnTree.emptyPrompt")}</span>
                      {node.isCurrent && <span className="turn-tree-card__badge">{t("turnTree.current")}</span>}
                    </div>
                  );
                })}
              </div>
            </div>
            {pendingNode && (
              <div
                className="turn-tree-action"
                role="dialog"
                aria-modal="false"
                aria-labelledby="turn-tree-action-title"
                style={{ left: actionPosition.x, top: actionPosition.y } as CSSProperties}
              >
                <div
                  className="turn-tree-action__title"
                  id="turn-tree-action-title"
                  onPointerDown={startActionDrag}
                  onPointerMove={dragAction}
                  onPointerUp={stopActionDrag}
                  onPointerCancel={stopActionDrag}
                >
                  {t("turnTree.actionTitle", { turn: pendingNode.turn + 1 })}
                </div>
                <div className="turn-tree-action__summary">
                  {pendingNode.prompt || t("turnTree.emptyPrompt")}
                </div>
                <div className="turn-tree-action__meta">
                  <span>{t("turnTree.prefixLabel")}</span>
                  {t("turnTree.prefixChars", { count: compactNumber(pendingNode.prefixChars) })}
                </div>
                {pendingNode.response && (
                  <div className="turn-tree-action__response">
                    <span>{t("turnTree.responseLabel")}</span>
                    {pendingNode.response}
                  </div>
                )}
                <div className="turn-tree-action__buttons">
                  <button className="btn btn--primary" type="button" disabled={pendingNode.isCurrent} onClick={() => void jump(pendingNode, "current")}>
                    {t("turnTree.openCurrent")}
                  </button>
                  <button className="btn" type="button" onClick={() => void jump(pendingNode, "new")}>
                    {t("turnTree.openNew")}
                  </button>
                  <button className="btn btn--ghost" type="button" onClick={() => setPendingNode(null)}>
                    {t("common.cancel")}
                  </button>
                </div>
              </div>
            )}
          </div>
        )}
      </div>
    </ResizableDrawer>
  );
}
