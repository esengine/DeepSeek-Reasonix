import type { DiagramDB } from '../../diagram-api/types.js';
/**
 * AST Node types for Railroad diagram expressions
 */
export interface TerminalNode {
    type: 'terminal';
    value: string;
}
export interface NonTerminalNode {
    type: 'nonterminal';
    name: string;
}
export interface SequenceNode {
    type: 'sequence';
    elements: ASTNode[];
}
export interface ChoiceNode {
    type: 'choice';
    alternatives: ASTNode[];
}
export interface OptionalNode {
    type: 'optional';
    element: ASTNode;
}
export interface RepetitionNode {
    type: 'repetition';
    element: ASTNode;
    min: number;
    max: number;
    separator?: ASTNode;
}
export interface SpecialNode {
    type: 'special';
    text: string;
}
/**
 * Union type for all AST nodes
 */
export type ASTNode = TerminalNode | NonTerminalNode | SequenceNode | ChoiceNode | OptionalNode | RepetitionNode | SpecialNode;
/**
 * Railroad diagram rule definition
 */
export interface RailroadRule {
    name: string;
    definition: ASTNode;
    comment?: string;
}
/**
 * Configuration for Railroad diagram styling
 */
export interface RailroadStyleOptions {
    compactMode?: boolean;
    padding?: number;
    verticalSeparation?: number;
    horizontalSeparation?: number;
    arcRadius?: number;
    fontSize?: number;
    fontFamily?: string;
    terminalFill?: string;
    terminalStroke?: string;
    terminalTextColor?: string;
    nonTerminalFill?: string;
    nonTerminalStroke?: string;
    nonTerminalTextColor?: string;
    lineColor?: string;
    strokeWidth?: number;
    markerFill?: string;
    commentFill?: string;
    commentStroke?: string;
    commentTextColor?: string;
    specialFill?: string;
    specialStroke?: string;
    ruleNameColor?: string;
    showMarkers?: boolean;
    markerRadius?: number;
}
/**
 * Railroad diagram configuration (alias for RailroadStyleOptions)
 */
export type RailroadDiagramConfig = RailroadStyleOptions;
/**
 * Default configuration
 */
export declare const DEFAULT_RAILROAD_CONFIG: Required<RailroadStyleOptions>;
/**
 * Diagram dimensions for rendering
 */
export interface DiagramDimensions {
    width: number;
    height: number;
    up: number;
    down: number;
}
/**
 * Render result containing SVG element and dimensions
 */
export interface RenderResult {
    element: SVGElement;
    dimensions: DiagramDimensions;
}
/**
 * Railroad diagram Database interface
 */
export interface RailroadDB extends DiagramDB {
    clear: () => void;
    setTitle: (title: string) => void;
    getTitle: () => string;
    addRule: (rule: RailroadRule) => void;
    getRules: () => RailroadRule[];
    getRule: (name: string) => RailroadRule | undefined;
    setAccTitle: (title: string) => void;
    getAccTitle: () => string;
    setAccDescription: (description: string) => void;
    getAccDescription: () => string;
    setDiagramTitle: (title: string) => void;
    getDiagramTitle: () => string;
}
