import type { Node } from '../../types.js';
export declare const solidStateFill: (color: string) => {
    fill: string;
    hachureAngle: number;
    hachureGap: number;
    fillWeight: number;
    roughness: number;
    stroke: string;
    seed: number | undefined;
};
export declare const compileStyles: (node: Node) => {
    stylesMap: Map<string, string>;
    stylesArray: [string, string][];
};
export declare const styles2Map: (styles: string[]) => Map<string, string>;
export declare const isLabelStyle: (key: string) => key is "font-family" | "font-size" | "font-weight" | "text-align" | "font-style" | "white-space" | "line-height" | "color" | "text-decoration" | "text-transform" | "letter-spacing" | "word-spacing" | "text-shadow" | "text-overflow" | "word-wrap" | "word-break" | "overflow-wrap" | "hyphens";
export declare const styles2String: (node: Node) => {
    labelStyles: string;
    nodeStyles: string;
    stylesArray: [string, string][];
    borderStyles: string[];
    backgroundStyles: string[];
};
export declare const userNodeOverrides: (node: Node, options: any) => any;
