/**
 * A min-priority queue data structure. This algorithm is derived from Cormen,
 * et al., "Introduction to Algorithms". The basic idea of a min-priority
 * queue is that you can efficiently (in O(1) time) get the smallest key in
 * the queue. Adding and removing elements takes O(log n) time. A key can
 * have its priority decreased in O(log n) time.
 */
export class PriorityQueue {
    /**
     * @private
     * @type {Array<{key: string, priority: number}>}
     */
    private _arr;
    /**
     * @private
     * @type {Record<string, number>}
     */
    private _keyIndices;
    /**
     * @returns {number} the number of elements in the queue.
     * @remarks Takes `O(1)` time.
     */
    size(): number;
    /**
     * @returns {string[]} the keys that are in the queue.
     * @remarks Takes `O(n)` time.
     */
    keys(): string[];
    /**
     * @param {Object} key - The key to check for presence in the queue.
     * @returns {boolean} `true` if **key** is in the queue and `false` if not.
     */
    has(key: any): boolean;
    /**
     * @param {Object} key - The key to get the priority for.
     * @returns {number | undefined} the priority for **key**.
     * If **key** is not present in the queue then this function returns `undefined`.
     * @remarks Takes `O(1)` time.
     */
    priority(key: any): number | undefined;
    /**
     * @returns {string} the key for the minimum element in this queue.
     * @throws {Error} if the queue is empty.
     * @remarks Takes `O(1)` time.
     */
    min(): string;
    /**
     * Inserts a new key into the priority queue.
     *
     * @remarks Takes `O(n)` time.
     *
     * @param {Object} key the key to add. This will be coerced to a `string`.
     * @param {Number} priority the initial priority for the key
     * @returns {boolean} `true` if the key was added and `false` if it was already
     * present in the queue.
     */
    add(key: any, priority: number): boolean;
    /**
     * Removes and returns the smallest key in the queue.
     * @returns {string} the key with the smallest priority
     * @remarks Takes `O(log n)` time.
     */
    removeMin(): string;
    /**
     * Decreases the priority for **key** to **priority**.
     *
     * @param {Object} key the key for which to raise priority
     * @param {Number} priority the new priority for the key
     * @throws {Error} if the new priority is  greater than the previous priority.
     */
    decrease(key: any, priority: number): void;
    /**
     * @param {number} i - Lower index.
     * @private
     */
    private _heapify;
    /**
     * @param {number} index - Index to decrease.
     * @private
     */
    private _decrease;
    /**
     * @param {number} i - First index
     * @param {number} j - Second index
     * @private
     */
    private _swap;
}
