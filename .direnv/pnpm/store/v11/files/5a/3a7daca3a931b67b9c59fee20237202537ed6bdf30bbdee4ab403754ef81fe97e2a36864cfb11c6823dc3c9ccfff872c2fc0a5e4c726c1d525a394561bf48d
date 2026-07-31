/**
 * Shared geometry helpers for layout validation and scoring.
 *
 * Extracted from validateLayout.ts so both validateLayout and scoreLayout
 * can reuse the same normalization, distance, and crossing logic.
 */
import type { Point } from './types.js';
/** General epsilon for coordinate comparisons */
export declare const EPS = 1;
export interface Segment {
    a: Point;
    b: Point;
    orientation: 'H' | 'V' | 'Z';
}
export interface NormalizedPolyline {
    points: Point[];
    segments: Segment[];
    bends: number;
}
/** Get segment orientation */
export declare function segmentOrientation(a: Point, b: Point): 'H' | 'V' | 'Z';
/** Create segments from consecutive point pairs */
export declare function segmentsFromPoints(points: Point[]): Segment[];
/** Merge consecutive collinear segments */
export declare function mergeCollinear(segments: Segment[]): Segment[];
/** Normalize a polyline: remove zero-length, merge collinear, count bends */
export declare function normalizePolyline(points: Point[]): NormalizedPolyline;
/** Euclidean distance between two points */
export declare function distance(a: Point, b: Point): number;
/**
 * Check if two orthogonal segments cross.
 *
 * Detects proper interior crossings as well as T-intersections where one
 * segment's endpoint lies on the other segment's interior.  The only case
 * that is excluded is a *shared endpoint*: when the intersection point is
 * an endpoint of **both** segments (two edges departing from the same node
 * corner should not count as a crossing).
 */
export declare function segmentsCross(s1: Segment, s2: Segment): boolean;
