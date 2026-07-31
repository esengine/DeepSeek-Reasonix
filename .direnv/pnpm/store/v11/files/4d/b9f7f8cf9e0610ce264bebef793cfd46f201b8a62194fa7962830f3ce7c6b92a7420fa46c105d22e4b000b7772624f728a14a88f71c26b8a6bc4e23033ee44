/** @template {ArrayLike<number>} T */
export default class Delaunator<T extends ArrayLike<number>> {
    /**
     * Constructs a delaunay triangulation object given an array of points (`[x, y]` by default).
     * `getX` and `getY` are optional functions of the form `(point) => value` for custom point formats.
     *
     * @template P
     * @param {P[]} points
     * @param {(p: P) => number} [getX]
     * @param {(p: P) => number} [getY]
     */
    static from<P>(points: P[], getX?: (p: P) => number, getY?: (p: P) => number): Delaunator<Float64Array<ArrayBuffer>>;
    /**
     * Constructs a delaunay triangulation object given an array of point coordinates of the form:
     * `[x0, y0, x1, y1, ...]` (use a typed array for best performance). Duplicate points are skipped.
     *
     * @param {T} coords
     */
    constructor(coords: T);
    coords: T;
    /** @private */ private _triangles;
    /** @private */ private _halfedges;
    /** @private */ private _hashSize;
    /** @private */ private _hullPrev;
    /** @private */ private _hullNext;
    /** @private */ private _hullTri;
    /** @private */ private _hullHash;
    /** @private */ private _ids;
    /** @private */ private _dists;
    /** @private */ private trianglesLen;
    /** @private */ private _cx;
    /** @private */ private _cy;
    /** @private */ private _hullStart;
    /** A `Uint32Array` array of indices that reference points on the convex hull of the input data, counter-clockwise. */
    hull: Uint32Array<ArrayBuffer>;
    /** A `Uint32Array` array of triangle vertex indices (each group of three numbers forms a triangle). All triangles are directed counterclockwise. */
    triangles: Uint32Array<ArrayBuffer>;
    /**
     * A `Int32Array` array of triangle half-edge indices that allows you to traverse the triangulation.
     * `i`-th half-edge in the array corresponds to vertex `triangles[i]` the half-edge is coming from.
     * `halfedges[i]` is the index of a twin half-edge in an adjacent triangle (or `-1` for outer half-edges on the convex hull).
     */
    halfedges: Int32Array<ArrayBuffer>;
    /**
     * Updates the triangulation if you modified `delaunay.coords` values in place, avoiding expensive memory allocations.
     * Useful for iterative relaxation algorithms such as Lloyd's.
     */
    update(): void;
    /**
     * Calculate an angle-based key for the edge hash used for advancing convex hull.
     *
     * @param {number} x
     * @param {number} y
     * @private
     */
    private _hashKey;
    /**
     * Flip an edge in a pair of triangles if it doesn't satisfy the Delaunay condition.
     *
     * @param {number} a
     * @private
     */
    private _legalize;
    /**
     * Link two half-edges to each other.
     * @param {number} a
     * @param {number} b
     * @private
     */
    private _link;
    /**
     * Add a new triangle given vertex indices and adjacent half-edge ids.
     *
     * @param {number} i0
     * @param {number} i1
     * @param {number} i2
     * @param {number} a
     * @param {number} b
     * @param {number} c
     * @private
     */
    private _addTriangle;
}
