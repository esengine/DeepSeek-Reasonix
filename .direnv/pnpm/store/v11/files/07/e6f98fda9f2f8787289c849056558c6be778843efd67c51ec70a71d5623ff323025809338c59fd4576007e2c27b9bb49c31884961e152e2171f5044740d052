/**
 * @file generational-cache.js
 * A generational pseudo-LRU cache with strict maximum size limits.
 */

/**
 * @template K, V
 */
export class GenerationalCache {
  #max;
  #boundary;
  #current = new Map();
  #old = new Map();

  /**
   * Initializes a new instance of the GenerationalCache class.
   * @param {number} max - The maximum number of items the cache can hold.
   */
  constructor(max) {
    this.max = max;
  }

  /**
   * Returns the total number of `entries` currently in the cache.
   * @note To optimize for write speed, this library allows temporary key
   * duplication between generations. Therefore, this value may not always
   * reflect the exact count of unique `keys`.
   * @returns {number} The total entry count.
   */
  get size() {
    return this.#current.size + this.#old.size;
  }

  /**
   * Returns the maximum capacity of the cache.
   * @returns {number} The maximum size limit.
   */
  get max() {
    return this.#max;
  }

  /**
   * Sets the maximum capacity of the cache and recalculates the boundary.
   * Clears the cache when updated.
   * @param {number} value - The new maximum capacity to set.
   */
  set max(value) {
    if (Number.isFinite(value) && value > 4) {
      this.#max = value;
      this.#boundary = Math.ceil(value / 2);
    } else {
      this.#max = 4;
      this.#boundary = 2;
    }
    this.clear();
  }

  /**
   * Retrieves an item from the cache.
   * If the item is in the older generation, it gets promoted to the current
   * generation.
   * @param {K} key - The key of the element to return.
   * @returns {V | undefined} The element associated with the specified key, or
   * undefined if the key cannot be found.
   */
  get(key) {
    let value = this.#current.get(key);
    if (value !== undefined) {
      return value;
    }
    value = this.#old.get(key);
    if (value !== undefined) {
      this.set(key, value);
      return value;
    }
    return undefined;
  }

  /**
   * Adds or updates an element with a specified key and a value to the cache.
   * @param {K} key - The key of the element to add.
   * @param {V} value - The value of the element to add.
   * @returns {GenerationalCache} The cache object itself.
   */
  set(key, value) {
    this.#current.set(key, value);
    // Swap generations if the current map reaches the boundary
    if (this.#current.size >= this.#boundary) {
      this.#old = this.#current;
      this.#current = new Map();
    }
    return this;
  }

  /**
   * Returns a boolean indicating whether an element with the specified key
   * exists or not.
   * @param {K} key - The key of the element to test for presence.
   * @returns {boolean} true if an element with the specified key exists in the
   * cache; otherwise false.
   */
  has(key) {
    return this.#current.has(key) || this.#old.has(key);
  }

  /**
   * Removes the specified element from the cache.
   * @param {K} key - The key of the element to remove.
   * @returns {boolean} true if an element in the cache existed and has been
   * removed, or false if the element does not exist.
   */
  delete(key) {
    const deletedFromCurrent = this.#current.delete(key);
    const deletedFromOld = this.#old.delete(key);
    return deletedFromCurrent || deletedFromOld;
  }

  /**
   * Removes all elements from the cache.
   */
  clear() {
    this.#current.clear();
    this.#old.clear();
  }
}
