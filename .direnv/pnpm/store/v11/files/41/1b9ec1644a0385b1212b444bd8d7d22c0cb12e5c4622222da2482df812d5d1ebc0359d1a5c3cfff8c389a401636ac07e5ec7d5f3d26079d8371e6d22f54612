import { GenerationalCache } from "@asamuzakjp/generational-cache";
//#region src/js/cache.ts
/**
* cache
*/
var CACHE_SIZE = 2048;
/**
* CacheItem
*/
var CacheItem = class {
	#isNull;
	#item;
	constructor(item, isNull = false) {
		this.#item = item;
		this.#isNull = !!isNull;
	}
	get item() {
		return this.#item;
	}
	get isNull() {
		return this.#isNull;
	}
};
/**
* NullObject
*/
var NullObject = class extends CacheItem {
	constructor() {
		super(Symbol("null"), true);
	}
};
var genCache = new GenerationalCache(CACHE_SIZE);
/**
* shared null object
*/
var sharedNullObject = new NullObject();
/**
* set cache
* @param key - cache key
* @param value - value to cache
* @returns void
*/
var setCache = (key, value) => {
	if (!key) return;
	if (value === null) genCache.set(key, sharedNullObject);
	else if (value instanceof CacheItem) genCache.set(key, value);
	else genCache.set(key, new CacheItem(value));
};
/**
* get cache
* @param key - cache key
* @returns cached item or false otherwise
*/
var getCache = (key) => {
	if (!key) return false;
	const item = genCache.get(key);
	if (item !== void 0) return item;
	return false;
};
/**
* helper function to sort object keys alphabetically
* @param obj - Object
* @returns stringified JSON
*/
var stringifySorted = (obj) => {
	const keys = Object.keys(obj);
	if (keys.length === 0) return "";
	keys.sort();
	let result = "";
	for (const key of keys) result += `${key}:${JSON.stringify(obj[key])};`;
	return result;
};
/**
* create cache key
* @param keyData - key data
* @param [opt] - options
* @returns cache key
*/
var createCacheKey = (keyData, opt = {}) => {
	if (!keyData || opt.customProperty && typeof opt.customProperty.callback === "function" || opt.dimension && typeof opt.dimension.callback === "function") return "";
	const namespace = keyData.namespace || "";
	const name = keyData.name || "";
	const value = keyData.value || "";
	if (!namespace && !name && !value) return "";
	return `${`${namespace}:${name}:${value}`}::${`${opt.format || ""}|${opt.colorSpace || ""}|${opt.colorScheme || ""}|${opt.currentColor || ""}|${opt.d50 ? "1" : "0"}|${opt.nullable ? "1" : "0"}|${opt.preserveComment ? "1" : "0"}|${opt.delimiter || ""}`}::${opt.customProperty ? stringifySorted(opt.customProperty) : ""}::${opt.dimension ? stringifySorted(opt.dimension) : ""}`;
};
//#endregion
export { CacheItem, NullObject, createCacheKey, getCache, setCache };

//# sourceMappingURL=cache.js.map