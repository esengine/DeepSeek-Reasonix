import { Selection } from 'd3-selection';

export interface ISetOverlap {
  sets: readonly string[];
  size: number;
  weight?: number;
}

export interface IPoint {
  x: number;
  y: number;
}

export interface IHierarchyPoint extends IPoint {
  parentIndex: [number, number];
}

export interface ICircle {
  x: number;
  y: number;
  radius: number;
  setid: string;
}

export interface ISolution {
  [set: string]: ICircle;
}

/**
 * given a list of set objects, and their corresponding overlaps
 * updates the(x, y, radius) attribute on each set such that their positions
 * roughly correspond to the desired overlaps
 */
export function venn(areas: readonly ISetOverlap[], parameters?: any): ISolution;

/**
 * Returns the distance necessary for two circles of radius r1 + r2 to
 * have the overlap area 'overlap'
 */
export function distanceFromIntersectArea(r1: number, r2: number, overlap: number): number;

/**
 * takes the best working variant of either constrained MDS or greedy
 */
export function bestInitialLayout(areas: readonly ISetOverlap[], params?: any): ISolution;

/**
 * Lays out a Venn diagram greedily, going from most overlapped sets to
 * least overlapped, attempting to position each new set such that the
 * overlapping areas to already positioned sets are basically right
 */
export function greedyLayout(areas: readonly ISetOverlap[], params?: any): ISolution;

/**
 * Given a bunch of sets, and the desired overlaps between these sets - computes
 * the distance from the actual overlaps to the desired overlaps. Note that
 * this method ignores overlaps of more than 2 circles
 */
export function lossFunction(circles: ISolution, areas: readonly ISetOverlap[]): number;

export function logRatioLossFunction(circles: ISolution, areas: readonly ISetOverlap[]): number;

export function disjointCluster(circles: ICircle[]): ICircle[][];

export function normalizeSolution(
  solution: ISolution,
  orientation?: number,
  orientationOrder?: (a: ICircle, b: ICircle) => number
): ISolution;

/**
 * Scales a solution from venn.venn or venn.greedyLayout such that it fits in
 * a rectangle of width/height - with padding around the borders. also
 * centers the diagram in the available space at the same time.
 * If the scale parameter is not null, this automatic scaling is ignored in favor of this custom one
 */
export function scaleSolution(
  solution: ISolution,
  width: number,
  height: number,
  padding: number,
  scaleToFit?: boolean
): ISolution;

/**
 * Returns the intersection area of a bunch of circles (where each circle
 * is an object having an x,y and radius property)
 */
export function intersectionArea(
  circles: readonly ICircle[],
  stats?: {
    area?: number;
    areaArea?: number;
    polygonArea?: number;
    arcs?: readonly { circle: ICircle; width: number; p1: IPoint; p2: IPoint }[];
    innerPoints?: readonly IHierarchyPoint[];
    intersectionPoints?: readonly IHierarchyPoint[];
  }
): number;

/**
 * Circular segment area calculation. See http://mathworld.wolfram.com/CircularSegment.html
 */
export function circleArea(r: number, width: number): number;

/**
 * euclidean distance between two points
 */
export function distance(p1: IPoint, p2: IPoint): number;

/**
 * Returns the overlap area of two circles of radius r1 and r2 - that
 * have their centers separated by distance d. Simpler faster
 * circle intersection for only two circles
 */
export function circleOverlap(r1: number, r2: number, d: number): number;

/**
 * Given two circles (containing a x/y/radius attributes),
 * returns the intersecting points if possible
 * note: doesn't handle cases where there are infinitely many
 * intersection points (circles are equivalent):, or only one intersection point
 */
export function circleCircleIntersection(p1: ICircle, p2: ICircle): readonly IPoint[];

// sometimes text doesn't fit inside the circle, if thats the case lets wrap
// the text here such that it fits
// todo: looks like this might be merged into d3 (
// https://github.com/mbostock/d3/issues/1642),
// also worth checking out is
// http://engineering.findthebest.com/wrapping-axis-labels-in-d3-js/
// this seems to be one of those things that should be easy but isn't
export function wrapText(circles: ISolution, labeller: (d: any) => string): (this: SVGTextElement) => void;

/**
 * compute the center of some circles by maximizing the margin of
 * the center point relative to the circles (interior) after subtracting
 * nearby circles (exterior)
 * @param {readonly {x: number, y: number, radius: number}[]} interior
 * @param {readonly {x: number, y: number, radius: number}[]} exterior
 * @param {boolean} symmetricalTextCentre
 * @returns {{x:number, y: number}}
 */
export function computeTextCentre(
  interior: readonly ICircle[],
  exterior: readonly ICircle[],
  symmetricalTextCentre?: boolean
): IPoint;

export function computeTextCentres(
  circles: ISolution,
  areas: readonly ISetOverlap[],
  symmetricalTextCentre?: boolean
): { [set: string]: IPoint };

// sorts all areas in the venn diagram, so that
// a particular area is on top (relativeTo) - and
// all other areas are so that the smallest areas are on top
export function sortAreas(div: Selection<any, any, any, unknown>, relativeTo: { sets: readonly string[] }): void;

export function circlePath(x: number, y: number, r: number): string;

/**
 * inverse of the circlePath function, returns a circle object from an svg path
 */
export function circleFromPath(path: string): ICircle;

/**
 * returns a svg path of the intersection area of a bunch of circles
 */
export function intersectionAreaPath(circles: readonly ICircle[]): string;

export interface IVennDiagramOptions {
  colourScheme?: readonly string[];
  colorScheme?: readonly string[];
  symmetricalTextCentre?: boolean;
  textFill?: string;
}

export interface IStyledSetOverlap extends ISetOverlap {
  color?: string;
  colour?: string;
}

export interface IVennDiagram {
  (selection: Selection<HTMLElement, readonly IStyledSetOverlap[], any, any>): {
    circles: ISolution;
    textCentres: { [set: string]: IPoint };
    nodes: Selection<SVGGElement, IStyledSetOverlap, any, unknown>;
    enter: Selection<SVGGElement, IStyledSetOverlap, any, unknown>;
    update: Selection<SVGGElement, IStyledSetOverlap, any, unknown>;
    exit: Selection<SVGGElement, IStyledSetOverlap, any, unknown>;
  };

  /**
   * wrap the labels
   * @default true
   */
  wrap(): boolean;
  wrap(v: boolean): this;
  /**
   * use a view box instead of specifying with and height
   * @default false
   */
  useViewBox(): this;

  width(): number;
  width(v: number): this;
  height(): number;
  height(v: number): this;
  padding(): number;
  padding(v: number): this;
  colours(): (key: string) => string;
  colours(v: (key: string) => string): this;
  colors(): (key: string) => string;
  colors(v: (key: string) => string): this;
  fontSize(): string | null;
  fontSize(v: string | null): this;

  duration(): number;
  duration(v: number): this;

  /**
   * normalize the solution
   * @default true
   */
  normalize(): boolean;
  normalize(v: boolean): this;
  layoutFunction(): typeof venn;
  layoutFunction(v: typeof venn): this;

  scaleToFit(): boolean;
  scaleToFit(v: boolean): this;

  styled(): boolean;
  styled(v: boolean): this;

  /**
   * round generate path to the given number of digits or null to disable
   * @default null
   */
  round(): number | null;
  round(v: number | null): this;

  /**
   * interpret the data as distinct sets, so set A is actually set A \ B u C
   * thus A is (A \ (B u C)) u (A ^ B \ C) u (A ^ C \ B) u (A ^ B ^ C)
   * @default false
   */
  distinct(): boolean;
  distinct(v: boolean): this;

  orientation(): number;
  orientation(v: number): this;
  orientationOrder(): null | ((a: ICircle, b: ICircle) => number);
  orientationOrder(v: null | ((a: ICircle, b: ICircle) => number)): this;

  lossFunction(): typeof lossFunction;
  lossFunction(v: 'logRatio'): this;
  lossFunction(v: 'default'): this;
  lossFunction(v: typeof lossFunction): this;
}

/**
 * VennDiagram includes an optional `options` parameter containing the following option(s):
 *
 * `colourScheme: Array<String>`
 * A list of color values to be applied when coloring diagram circles.
 *
 * `symmetricalTextCentre: Boolean`
 * Whether to symmetrically center each circle's text horizontally and vertically.
 * Defaults to `false`.
 *
 * `textFill: String`
 * The color to be applied to the text within each circle.
 */
export function VennDiagram(options?: IVennDiagramOptions): IVennDiagram;

export interface IComputeVennLayoutOptions {
  width?: number;
  height?: number;
  padding?: number;
  /**
   * @default true
   */
  normalize?: boolean;
  layoutFunction?: typeof venn;
  lossFunction?: typeof lossFunction | 'logRatio';
  scaleToFit?: boolean;
  orientation?: number;
  distinct?: boolean;
  /**
   * @default 2
   */
  round?: number;
  orientationOrder?: (a: ICircle, b: ICircle) => number;
}

export interface IVennLayout<T> {
  data: T;
  /**
   * text location
   */
  text: IPoint;
  circles: readonly (ICircle & { set: string })[];
  arcs: readonly { circle: ICircle; width: number; p1: IPoint; p2: IPoint; large: boolean; sweep: boolean }[];
  /**
   * SVG path
   */
  path: string;
  /**
   * distinct SVG path, render with `fill-rule: evenodd`;
   */
  distinctPath: string;
}
export function layout<T extends ISetOverlap>(
  data: readonly T[],
  options?: IComputeVennLayoutOptions
): IVennLayout<T>[];
