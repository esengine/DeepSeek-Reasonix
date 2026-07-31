export function positionX(g: any): {
    [x: string]: any;
};
export function findType1Conflicts(g: any, layering: any): {
    [nodeId: string]: {
        [nodeId: string]: true;
        [nodeId: number]: true;
    };
    [nodeId: number]: {
        [nodeId: string]: true;
        [nodeId: number]: true;
    };
};
export function findType2Conflicts(g: any, layering: any): {
    [nodeId: string]: {
        [nodeId: string]: true;
        [nodeId: number]: true;
    };
    [nodeId: number]: {
        [nodeId: string]: true;
        [nodeId: number]: true;
    };
};
/**
 * Sets `conflicts[v][w] = true`, creating objects if needed.
 *
 * @param {{[nodeId: string | number]: {[nodeId: string | number]: true}}} conflicts - Object to set.
 * @param {string | number} v - First Node ID
 * @param {string | number} w - Second Node ID
 */
export function addConflict(conflicts: {
    [nodeId: string | number]: {
        [nodeId: string | number]: true;
    };
}, v: string | number, w: string | number): void;
export function hasConflict(conflicts: any, v: any, w: any): any;
export function verticalAlignment(g: any, layering: any, conflicts: any, neighborFn: any): {
    root: {};
    align: {};
};
export function horizontalCompaction(g: any, layering: any, root: any, align: any, reverseSep: any): Record<string, number>;
export function alignCoordinates(xss: any, alignTo: any): void;
export function findSmallestWidthAlignment(g: any, xss: any): any;
export function balance(xss: any, align: any): {
    [x: string]: any;
};
