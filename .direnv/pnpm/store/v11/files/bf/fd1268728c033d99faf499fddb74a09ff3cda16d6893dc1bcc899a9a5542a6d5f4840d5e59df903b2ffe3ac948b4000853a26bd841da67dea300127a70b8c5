export interface WardleyNode {
    id: string;
    label: string;
    x?: number;
    y?: number;
    className?: string;
    labelOffsetX?: number;
    labelOffsetY?: number;
    inPipeline?: boolean;
    isPipelineParent?: boolean;
    inertia?: boolean;
    sourceStrategy?: 'build' | 'buy' | 'outsource' | 'market';
}
export interface WardleyLink {
    source: string;
    target: string;
    dashed?: boolean;
    label?: string;
    flow?: 'forward' | 'backward' | 'bidirectional';
}
export interface WardleyTrend {
    nodeId: string;
    targetX: number;
    targetY: number;
}
export interface WardleyPipeline {
    nodeId: string;
    componentIds: string[];
}
export interface WardleyAnnotation {
    number: number;
    coordinates: {
        x: number;
        y: number;
    }[];
    text?: string;
}
export interface WardleyNote {
    text: string;
    x: number;
    y: number;
}
export interface WardleyAccelerator {
    name: string;
    x: number;
    y: number;
}
export interface WardleyDeaccelerator {
    name: string;
    x: number;
    y: number;
}
export interface WardleyAxesConfig {
    xLabel?: string;
    yLabel?: string;
    stages?: string[];
    stageBoundaries?: number[];
}
export interface WardleyBuildResult {
    nodes: WardleyNode[];
    links: WardleyLink[];
    trends: WardleyTrend[];
    pipelines: WardleyPipeline[];
    annotations: WardleyAnnotation[];
    notes: WardleyNote[];
    accelerators: WardleyAccelerator[];
    deaccelerators: WardleyDeaccelerator[];
    annotationsBox?: {
        x: number;
        y: number;
    };
    axes: WardleyAxesConfig;
    size?: {
        width: number;
        height: number;
    };
}
export declare class WardleyBuilder {
    private nodes;
    private links;
    private trends;
    private pipelines;
    private annotations;
    private notes;
    private accelerators;
    private deaccelerators;
    private annotationsBox?;
    private axes;
    private size?;
    addNode(node: WardleyNode): void;
    addLink(link: WardleyLink): void;
    addTrend(trend: WardleyTrend): void;
    startPipeline(nodeId: string): void;
    addPipelineComponent(pipelineNodeId: string, componentId: string): void;
    addAnnotation(annotation: WardleyAnnotation): void;
    addNote(note: WardleyNote): void;
    addAccelerator(accelerator: WardleyAccelerator): void;
    addDeaccelerator(deaccelerator: WardleyDeaccelerator): void;
    setAnnotationsBox(x: number, y: number): void;
    setAxes(partial: WardleyAxesConfig): void;
    setSize(width: number, height: number): void;
    getNode(id: string): WardleyNode | undefined;
    /**
     * Resolve a name to a node ID. Tries exact ID match first,
     * then falls back to finding a node whose label matches the name
     * (handles pipeline components which have synthetic IDs like "Parent_Child").
     */
    resolveNodeId(name: string): string;
    build(): WardleyBuildResult;
    clear(): void;
}
