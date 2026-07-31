export type MarkdownWordType = 'normal' | 'strong' | 'em';
import type { MermaidConfig } from '../config.type.js';
import type { ClusterShapeID } from './rendering-elements/clusters.js';
import type { ShapeID } from './rendering-elements/shapes.js';
import type { Bounds, Point } from '../types.js';
export interface MarkdownWord {
    content: string;
    type: MarkdownWordType;
}
export type MarkdownLine = MarkdownWord[];
/** Returns `true` if the line fits a constraint (e.g. it's under 𝑛 chars) */
export type CheckFitFunction = (text: MarkdownLine) => boolean;
interface BaseNode {
    id: string;
    label?: string;
    description?: string[];
    parentId?: string;
    position?: string;
    cssStyles?: string[];
    cssCompiledStyles?: string[];
    cssClasses?: string;
    labelStyle?: string;
    labelType?: string;
    domId?: string;
    dir?: string;
    explicitDir?: boolean;
    haveCallback?: boolean;
    link?: string;
    linkTarget?: string;
    tooltip?: string;
    padding?: number;
    isGroup?: boolean;
    width?: number;
    height?: number;
    /** Optional rendered title/header region for group-like containers. */
    groupTitleRect?: GroupTitleRect;
    intersect?: (point: any) => any;
    calcIntersect?: (bounds: Bounds, point: Point) => any;
    rx?: number;
    ry?: number;
    useHtmlLabels?: boolean;
    centerLabel?: boolean;
    backgroundColor?: string;
    borderColor?: string;
    borderStyle?: string;
    borderWidth?: number;
    labelTextColor?: string;
    labelPaddingX?: number;
    labelPaddingY?: number;
    x?: number;
    y?: number;
    look?: string;
    icon?: string;
    pos?: 't' | 'b';
    img?: string;
    assetWidth?: number;
    assetHeight?: number;
    defaultWidth?: number;
    imageAspectRatio?: number;
    constraint?: 'on' | 'off';
    layer?: number;
    order?: number;
    children?: NodeChildren;
    nodeId?: string;
    level?: number;
    descr?: string;
    type?: number;
    radius?: number;
    taper?: number;
    stroke?: string;
    colorIndex?: number;
}
/**
 * Group/cluster nodes, e.g. nodes that contain other nodes.
 */
export type NodeChildren = Node[];
export interface GroupTitleRect {
    left: number;
    right: number;
    top: number;
    bottom: number;
}
export interface ClusterNode extends BaseNode {
    shape?: ClusterShapeID;
    isGroup: true;
    isEdgeLabel?: boolean;
    edgeStart?: string;
    edgeEnd?: string;
    isDummy?: boolean;
}
export interface NonClusterNode extends BaseNode {
    shape?: ShapeID;
    isGroup: false;
    isEdgeLabel?: boolean;
    edgeStart?: string;
    edgeEnd?: string;
    isDummy?: boolean;
}
export type Node = ClusterNode | NonClusterNode;
export interface Edge {
    id: string;
    label?: string;
    classes?: string;
    style?: string[];
    animate?: boolean;
    animation?: 'fast' | 'slow';
    arrowhead?: string;
    arrowheadStyle?: string;
    arrowTypeEnd?: string;
    arrowTypeStart?: string;
    cssCompiledStyles?: string[];
    defaultInterpolate?: string;
    end?: string;
    interpolate?: string;
    labelType?: string;
    length?: number;
    start?: string;
    stroke?: string;
    text?: string;
    type?: string;
    startLabelRight?: string;
    endLabelLeft?: string;
    curve?: string;
    labelpos?: string;
    labelStyle?: string[];
    minlen?: number;
    pattern?: string;
    thickness?: 'normal' | 'thick' | 'invisible' | 'dotted';
    look?: string;
    isUserDefinedId?: boolean;
    showPoints?: boolean;
    points?: Point[];
    parentId?: string;
    dir?: string;
    source?: string;
    target?: string;
    depth?: number;
    isLabelEdge?: boolean;
    /**
     * Label-as-waypoint marker: when set, the edge's polyline is routed through
     * the center of the label node with this id. Used by the swimlane router to
     * thread an original labelled edge through its `edge-label-*` node without
     * splitting the edge into two rendered sub-edges.
     */
    labelNodeId?: string;
    /**
     * Layout-only virtual edge: exists solely to feed Sugiyama layering /
     * ordering (e.g. A→label, label→B for swimlane label placement). Consumers
     * that route or render edges must skip any edge with `isLayoutOnly: true`.
     */
    isLayoutOnly?: boolean;
}
export interface RectOptions {
    rx: number;
    ry: number;
    labelPaddingX: number;
    labelPaddingY: number;
    classes: string;
}
export interface MindmapOptions {
    padding: number;
}
export type ClassDiagramNode = Node & {
    memberData: any;
};
export interface LayoutData {
    nodes: Node[];
    edges: Edge[];
    config: MermaidConfig;
    diagramId?: string;
    [key: string]: any;
}
export interface RenderData {
    items: (Node | Edge)[];
    [key: string]: any;
}
export type LayoutMethod = 'dagre' | 'dagre-wrapper' | 'elk' | 'neato' | 'dot' | 'circo' | 'fdp' | 'osage' | 'grid';
export interface ShapeRenderOptions {
    config: MermaidConfig;
    /** Some shapes render differently if a diagram has a direction `LR` */
    dir?: Node['dir'];
    padding?: number;
}
export type KanbanNode = Node & {
    priority?: 'Very High' | 'High' | 'Medium' | 'Low' | 'Very Low';
    ticket?: string;
    assigned?: string;
    icon?: string;
    level: number;
};
export {};
