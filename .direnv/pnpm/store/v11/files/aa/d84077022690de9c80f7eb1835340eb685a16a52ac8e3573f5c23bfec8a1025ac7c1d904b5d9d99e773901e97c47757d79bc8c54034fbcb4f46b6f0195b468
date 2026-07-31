/**
 * Shared utility functions for the orthogonal routing pipeline.
 *
 * This module contains geometry helpers, distance calculations, and other
 * low-level utilities used across the routing stages.
 */
import type { Node } from '../../types.js';
import type { Point, Rect, PortSide } from './types.js';
/**
 * Create a Rect from a Node's x, y, width, height properties.
 * The node's x, y are assumed to be the center coordinates.
 */
export declare function rectForNode(node: Node): Rect;
/**
 * Check if a point is strictly inside a rectangle (not on the boundary).
 */
export declare function pointInRectInterior(p: Point, rect: Rect): boolean;
/**
 * Approximate floating-point equality check.
 */
export declare function approxEqual(a: number, b: number, tol?: number): boolean;
/**
 * Calculate the total Manhattan length of a polyline.
 */
export declare function manhattanLength(points: Point[]): number;
/**
 * Calculate the Manhattan distance between two points.
 */
export declare function manhattanDistance(a: Point, b: Point): number;
/**
 * Count the number of bends (direction changes) in a polyline.
 */
export declare function bendCount(points: Point[]): number;
/**
 * Check if an axis-aligned segment intersects a rectangle's interior.
 * Non-orthogonal segments are ignored (return false).
 */
export declare function segmentIntersectsRectInterior(a: Point, b: Point, rect: Rect): boolean;
/**
 * Check if a polyline crosses a single rectangle.
 */
export declare function polylineIntersectsRect(points: Point[], rect: Rect): boolean;
/**
 * Check if a polyline crosses an array of obstacle rectangles.
 */
export declare function polylineIntersectsRects(points: Point[], rects: Rect[]): boolean;
export declare function pointInsideAnyRectInterior(p: Point, rects: Rect[]): boolean;
export declare function clamp(v: number, lo: number, hi: number): number;
/**
 * Remove duplicates from a sorted number array.
 */
export declare function uniqSorted(numbers: number[]): number[];
/**
 * Compute the boundary port point for a side of a rectangle.
 */
export declare function computeBoundaryPort(rect: Rect, side: PortSide): Point;
