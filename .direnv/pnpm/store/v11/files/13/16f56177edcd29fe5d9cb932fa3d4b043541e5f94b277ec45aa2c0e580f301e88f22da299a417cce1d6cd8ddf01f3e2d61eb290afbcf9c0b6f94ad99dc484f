/**
 * cache
 */

import { GenerationalCache } from '@asamuzakjp/generational-cache';
import { Options } from './typedef';

/* constants */
const CACHE_SIZE = 2048;

/**
 * CacheItem
 */
export class CacheItem {
  /* private */
  #isNull: boolean;
  #item: unknown;

  constructor(item: unknown, isNull: boolean = false) {
    this.#item = item;
    this.#isNull = !!isNull;
  }

  get item() {
    return this.#item;
  }

  get isNull() {
    return this.#isNull;
  }
}

/**
 * NullObject
 */
export class NullObject extends CacheItem {
  constructor() {
    super(Symbol('null'), true);
  }
}

/*
 * generational cache instance
 */
export const genCache = new GenerationalCache<string, CacheItem>(CACHE_SIZE);

/**
 * shared null object
 */
const sharedNullObject = new NullObject();

/**
 * set cache
 * @param key - cache key
 * @param value - value to cache
 * @returns void
 */
export const setCache = (key: string, value: unknown): void => {
  if (!key) {
    return;
  }
  if (value === null) {
    genCache.set(key, sharedNullObject);
  } else if (value instanceof CacheItem) {
    genCache.set(key, value);
  } else {
    genCache.set(key, new CacheItem(value));
  }
};

/**
 * get cache
 * @param key - cache key
 * @returns cached item or false otherwise
 */
export const getCache = (key: string): CacheItem | false => {
  if (!key) {
    return false;
  }
  const item = genCache.get(key);
  if (item !== undefined) {
    return item as CacheItem;
  }
  return false;
};

/**
 * helper function to sort object keys alphabetically
 * @param obj - Object
 * @returns stringified JSON
 */
const stringifySorted = (obj: Record<string, unknown>): string => {
  const keys = Object.keys(obj);
  if (keys.length === 0) {
    return '';
  }
  keys.sort();
  let result = '';
  for (const key of keys) {
    result += `${key}:${JSON.stringify(obj[key])};`;
  }
  return result;
};

/**
 * create cache key
 * @param keyData - key data
 * @param [opt] - options
 * @returns cache key
 */
export const createCacheKey = (
  keyData: Record<string, string>,
  opt: Options = {}
): string => {
  if (
    !keyData ||
    (opt.customProperty && typeof opt.customProperty.callback === 'function') ||
    (opt.dimension && typeof opt.dimension.callback === 'function')
  ) {
    return '';
  }
  const namespace = keyData.namespace || '';
  const name = keyData.name || '';
  const value = keyData.value || '';
  if (!namespace && !name && !value) {
    return '';
  }
  const baseKey = `${namespace}:${name}:${value}`;
  const optStr = `${opt.format || ''}|${opt.colorSpace || ''}|${opt.colorScheme || ''}|${opt.currentColor || ''}|${opt.d50 ? '1' : '0'}|${opt.nullable ? '1' : '0'}|${opt.preserveComment ? '1' : '0'}|${opt.delimiter || ''}`;
  const customPropStr = opt.customProperty
    ? stringifySorted(opt.customProperty as Record<string, unknown>)
    : '';
  const dimStr = opt.dimension
    ? stringifySorted(opt.dimension as Record<string, unknown>)
    : '';
  return `${baseKey}::${optStr}::${customPropStr}::${dimStr}`;
};
