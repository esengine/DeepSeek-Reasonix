/**
 * Deterministic pseudo-random number generator (mulberry32).
 * Returns a function that produces values in [0, 1).
 */
export declare function seededRandom(seed: number): number;
/**
 * Simple string hash for seeding the PRNG.
 */
export declare function hashString(str: string): number;
/**
 * Resolve the boundary-waviness seed. A non-zero configured seed takes
 * precedence so renders are reproducible (visual regression tests). When the
 * configured seed is 0 or absent we fall back to hashing the SVG element id,
 * which gives each rendered diagram its own waviness pattern at the cost of
 * being unstable across renders.
 */
export declare function resolveSeed(configuredSeed: number | undefined, id: string): number;
/**
 * Generate a vertical wavy line (the "fold") through the center of the diagram.
 * Uses cubic bezier segments with alternating control point offsets.
 * @param width - diagram width
 * @param height - diagram height
 * @param seed - deterministic seed for variation
 * @param amplitudeOverride - optional amplitude in pixels; falls back to 1.5% of width
 * @returns SVG path d attribute string
 */
export declare function generateFoldPath(width: number, height: number, seed: number, amplitudeOverride?: number): string;
/**
 * Generate a horizontal wavy line through the center of the diagram.
 * @param width - diagram width
 * @param height - diagram height
 * @param seed - deterministic seed for variation
 * @param amplitudeOverride - optional amplitude in pixels; falls back to 1.5% of height
 * @returns SVG path d attribute string
 */
export declare function generateHorizontalBoundary(width: number, height: number, seed: number, amplitudeOverride?: number): string;
/**
 * Generate the "cliff" path between Clear (bottom-right) and Chaotic (bottom-left).
 * This is a thicker, more abrupt boundary near the bottom center.
 * @param width - diagram width
 * @param height - diagram height
 * @returns SVG path d attribute string
 */
export declare function generateCliffPath(width: number, height: number): string;
/**
 * Generate an ellipse SVG path for the confusion/disorder region at the center.
 * @param cx - center x
 * @param cy - center y
 * @param rx - horizontal radius
 * @param ry - vertical radius
 * @returns SVG path d attribute string for an ellipse
 */
export declare function generateConfusionPath(cx: number, cy: number, rx: number, ry: number): string;
