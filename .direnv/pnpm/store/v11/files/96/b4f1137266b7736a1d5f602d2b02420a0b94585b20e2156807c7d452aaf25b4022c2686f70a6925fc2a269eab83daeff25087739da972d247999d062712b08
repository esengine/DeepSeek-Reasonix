import {
  require_dist
} from "./chunk-IPM4HZQ6.mjs";
import {
  assignWithDepth_default,
  common_default,
  detectType,
  directiveRegex,
  sanitizeDirective
} from "./chunk-CHAKFXHA.mjs";
import {
  basisClosed_default,
  basisOpen_default,
  basis_default,
  bumpX,
  bumpY,
  bundle_default,
  cardinalClosed_default,
  cardinalOpen_default,
  cardinal_default,
  catmullRomClosed_default,
  catmullRomOpen_default,
  catmullRom_default,
  linearClosed_default,
  linear_default,
  log,
  monotoneX,
  monotoneY,
  natural_default,
  select_default,
  stepAfter,
  stepBefore,
  step_default
} from "./chunk-FO5PYUIK.mjs";
import {
  __name,
  __toESM
} from "./chunk-PTVI3W5X.mjs";

// src/utils.ts
var import_sanitize_url = __toESM(require_dist(), 1);

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/predicate/isLength.mjs
function isLength(value) {
  return Number.isSafeInteger(value) && value >= 0;
}
__name(isLength, "isLength");

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/compat/predicate/isArrayLike.mjs
function isArrayLike(value) {
  return value != null && typeof value !== "function" && isLength(value.length);
}
__name(isArrayLike, "isArrayLike");

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/_internal/isUnsafeProperty.mjs
function isUnsafeProperty(key) {
  return key === "__proto__";
}
__name(isUnsafeProperty, "isUnsafeProperty");

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/predicate/isPrimitive.mjs
function isPrimitive(value) {
  return value == null || typeof value !== "object" && typeof value !== "function";
}
__name(isPrimitive, "isPrimitive");

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/compat/_internal/getSymbols.mjs
function getSymbols(object) {
  return Object.getOwnPropertySymbols(object).filter((symbol) => Object.prototype.propertyIsEnumerable.call(object, symbol));
}
__name(getSymbols, "getSymbols");

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/compat/_internal/getTag.mjs
function getTag(value) {
  if (value == null) {
    return value === void 0 ? "[object Undefined]" : "[object Null]";
  }
  return Object.prototype.toString.call(value);
}
__name(getTag, "getTag");

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/compat/_internal/tags.mjs
var regexpTag = "[object RegExp]";
var stringTag = "[object String]";
var numberTag = "[object Number]";
var booleanTag = "[object Boolean]";
var argumentsTag = "[object Arguments]";
var symbolTag = "[object Symbol]";
var dateTag = "[object Date]";
var mapTag = "[object Map]";
var setTag = "[object Set]";
var arrayTag = "[object Array]";
var arrayBufferTag = "[object ArrayBuffer]";
var objectTag = "[object Object]";
var dataViewTag = "[object DataView]";
var uint8ArrayTag = "[object Uint8Array]";
var uint8ClampedArrayTag = "[object Uint8ClampedArray]";
var uint16ArrayTag = "[object Uint16Array]";
var uint32ArrayTag = "[object Uint32Array]";
var int8ArrayTag = "[object Int8Array]";
var int16ArrayTag = "[object Int16Array]";
var int32ArrayTag = "[object Int32Array]";
var float32ArrayTag = "[object Float32Array]";
var float64ArrayTag = "[object Float64Array]";

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/predicate/isTypedArray.mjs
function isTypedArray(x) {
  return ArrayBuffer.isView(x) && !(x instanceof DataView);
}
__name(isTypedArray, "isTypedArray");

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/object/cloneDeepWith.mjs
function cloneDeepWith(obj, cloneValue) {
  return cloneDeepWithImpl(obj, void 0, obj, /* @__PURE__ */ new Map(), cloneValue);
}
__name(cloneDeepWith, "cloneDeepWith");
function cloneDeepWithImpl(valueToClone, keyToClone, objectToClone, stack = /* @__PURE__ */ new Map(), cloneValue = void 0) {
  const cloned = cloneValue?.(valueToClone, keyToClone, objectToClone, stack);
  if (cloned !== void 0) {
    return cloned;
  }
  if (isPrimitive(valueToClone)) {
    return valueToClone;
  }
  if (stack.has(valueToClone)) {
    return stack.get(valueToClone);
  }
  if (Array.isArray(valueToClone)) {
    const result = new Array(valueToClone.length);
    stack.set(valueToClone, result);
    for (let i = 0; i < valueToClone.length; i++) {
      result[i] = cloneDeepWithImpl(valueToClone[i], i, objectToClone, stack, cloneValue);
    }
    if (Object.hasOwn(valueToClone, "index")) {
      result.index = valueToClone.index;
    }
    if (Object.hasOwn(valueToClone, "input")) {
      result.input = valueToClone.input;
    }
    return result;
  }
  if (valueToClone instanceof Date) {
    return new Date(valueToClone.getTime());
  }
  if (valueToClone instanceof RegExp) {
    const result = new RegExp(valueToClone.source, valueToClone.flags);
    result.lastIndex = valueToClone.lastIndex;
    return result;
  }
  if (valueToClone instanceof Map) {
    const result = /* @__PURE__ */ new Map();
    stack.set(valueToClone, result);
    for (const [key, value] of valueToClone) {
      result.set(key, cloneDeepWithImpl(value, key, objectToClone, stack, cloneValue));
    }
    return result;
  }
  if (valueToClone instanceof Set) {
    const result = /* @__PURE__ */ new Set();
    stack.set(valueToClone, result);
    for (const value of valueToClone) {
      result.add(cloneDeepWithImpl(value, void 0, objectToClone, stack, cloneValue));
    }
    return result;
  }
  if (typeof Buffer !== "undefined" && Buffer.isBuffer(valueToClone)) {
    return valueToClone.subarray();
  }
  if (isTypedArray(valueToClone)) {
    const result = new (Object.getPrototypeOf(valueToClone)).constructor(valueToClone.length);
    stack.set(valueToClone, result);
    for (let i = 0; i < valueToClone.length; i++) {
      result[i] = cloneDeepWithImpl(valueToClone[i], i, objectToClone, stack, cloneValue);
    }
    return result;
  }
  if (valueToClone instanceof ArrayBuffer || typeof SharedArrayBuffer !== "undefined" && valueToClone instanceof SharedArrayBuffer) {
    return valueToClone.slice(0);
  }
  if (valueToClone instanceof DataView) {
    const result = new DataView(valueToClone.buffer.slice(0), valueToClone.byteOffset, valueToClone.byteLength);
    stack.set(valueToClone, result);
    copyProperties(result, valueToClone, objectToClone, stack, cloneValue);
    return result;
  }
  if (typeof File !== "undefined" && valueToClone instanceof File) {
    const result = new File([valueToClone], valueToClone.name, {
      type: valueToClone.type
    });
    stack.set(valueToClone, result);
    copyProperties(result, valueToClone, objectToClone, stack, cloneValue);
    return result;
  }
  if (typeof Blob !== "undefined" && valueToClone instanceof Blob) {
    const result = new Blob([valueToClone], { type: valueToClone.type });
    stack.set(valueToClone, result);
    copyProperties(result, valueToClone, objectToClone, stack, cloneValue);
    return result;
  }
  if (valueToClone instanceof Error) {
    const result = structuredClone(valueToClone);
    stack.set(valueToClone, result);
    result.message = valueToClone.message;
    result.name = valueToClone.name;
    result.stack = valueToClone.stack;
    result.cause = valueToClone.cause;
    result.constructor = valueToClone.constructor;
    copyProperties(result, valueToClone, objectToClone, stack, cloneValue);
    return result;
  }
  if (valueToClone instanceof Boolean) {
    const result = new Boolean(valueToClone.valueOf());
    stack.set(valueToClone, result);
    copyProperties(result, valueToClone, objectToClone, stack, cloneValue);
    return result;
  }
  if (valueToClone instanceof Number) {
    const result = new Number(valueToClone.valueOf());
    stack.set(valueToClone, result);
    copyProperties(result, valueToClone, objectToClone, stack, cloneValue);
    return result;
  }
  if (valueToClone instanceof String) {
    const result = new String(valueToClone.valueOf());
    stack.set(valueToClone, result);
    copyProperties(result, valueToClone, objectToClone, stack, cloneValue);
    return result;
  }
  if (typeof valueToClone === "object" && isCloneableObject(valueToClone)) {
    const result = Object.create(Object.getPrototypeOf(valueToClone));
    stack.set(valueToClone, result);
    copyProperties(result, valueToClone, objectToClone, stack, cloneValue);
    return result;
  }
  return valueToClone;
}
__name(cloneDeepWithImpl, "cloneDeepWithImpl");
function copyProperties(target, source, objectToClone = target, stack, cloneValue) {
  const keys = [...Object.keys(source), ...getSymbols(source)];
  for (let i = 0; i < keys.length; i++) {
    const key = keys[i];
    const descriptor = Object.getOwnPropertyDescriptor(target, key);
    if (descriptor == null || descriptor.writable) {
      target[key] = cloneDeepWithImpl(source[key], key, objectToClone, stack, cloneValue);
    }
  }
}
__name(copyProperties, "copyProperties");
function isCloneableObject(object) {
  switch (getTag(object)) {
    case argumentsTag:
    case arrayTag:
    case arrayBufferTag:
    case dataViewTag:
    case booleanTag:
    case dateTag:
    case float32ArrayTag:
    case float64ArrayTag:
    case int8ArrayTag:
    case int16ArrayTag:
    case int32ArrayTag:
    case mapTag:
    case numberTag:
    case objectTag:
    case regexpTag:
    case setTag:
    case stringTag:
    case symbolTag:
    case uint8ArrayTag:
    case uint8ClampedArrayTag:
    case uint16ArrayTag:
    case uint32ArrayTag: {
      return true;
    }
    default: {
      return false;
    }
  }
}
__name(isCloneableObject, "isCloneableObject");

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/compat/object/cloneDeepWith.mjs
function cloneDeepWith2(obj, customizer) {
  return cloneDeepWith(obj, (value, key, object, stack) => {
    const cloned = customizer?.(value, key, object, stack);
    if (cloned !== void 0) {
      return cloned;
    }
    if (typeof obj !== "object") {
      return void 0;
    }
    if (getTag(obj) === objectTag && typeof obj.constructor !== "function") {
      const result = {};
      stack.set(obj, result);
      copyProperties(result, obj, object, stack);
      return result;
    }
    switch (Object.prototype.toString.call(obj)) {
      case numberTag:
      case stringTag:
      case booleanTag: {
        const result = new obj.constructor(obj?.valueOf());
        copyProperties(result, obj);
        return result;
      }
      case argumentsTag: {
        const result = {};
        copyProperties(result, obj);
        result.length = obj.length;
        result[Symbol.iterator] = obj[Symbol.iterator];
        return result;
      }
      default: {
        return void 0;
      }
    }
  });
}
__name(cloneDeepWith2, "cloneDeepWith");

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/compat/object/cloneDeep.mjs
function cloneDeep(obj) {
  return cloneDeepWith2(obj);
}
__name(cloneDeep, "cloneDeep");

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/compat/predicate/isArguments.mjs
function isArguments(value) {
  return value !== null && typeof value === "object" && getTag(value) === "[object Arguments]";
}
__name(isArguments, "isArguments");

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/compat/predicate/isObjectLike.mjs
function isObjectLike(value) {
  return typeof value === "object" && value !== null;
}
__name(isObjectLike, "isObjectLike");

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/compat/predicate/isArrayLikeObject.mjs
function isArrayLikeObject(value) {
  return isObjectLike(value) && isArrayLike(value);
}
__name(isArrayLikeObject, "isArrayLikeObject");

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/compat/predicate/isArray.mjs
function isArray(value) {
  return Array.isArray(value);
}
__name(isArray, "isArray");

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/compat/function/memoize.mjs
function memoize(func, resolver) {
  if (typeof func !== "function" || resolver != null && typeof resolver !== "function") {
    throw new TypeError("Expected a function");
  }
  const memoized = /* @__PURE__ */ __name(function(...args) {
    const key = resolver ? resolver.apply(this, args) : args[0];
    const cache = memoized.cache;
    if (cache.has(key)) {
      return cache.get(key);
    }
    const result = func.apply(this, args);
    memoized.cache = cache.set(key, result) || cache;
    return result;
  }, "memoized");
  const CacheConstructor = memoize.Cache || Map;
  memoized.cache = new CacheConstructor();
  return memoized;
}
__name(memoize, "memoize");
memoize.Cache = Map;

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/function/noop.mjs
function noop() {
}
__name(noop, "noop");

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/compat/_internal/isPrototype.mjs
function isPrototype(value) {
  const constructor = value?.constructor;
  const prototype = typeof constructor === "function" ? constructor.prototype : Object.prototype;
  return value === prototype;
}
__name(isPrototype, "isPrototype");

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/compat/predicate/isTypedArray.mjs
function isTypedArray2(x) {
  return isTypedArray(x);
}
__name(isTypedArray2, "isTypedArray");

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/compat/object/clone.mjs
function clone(obj) {
  if (isPrimitive(obj)) {
    return obj;
  }
  const tag = getTag(obj);
  if (!isCloneableObject2(obj)) {
    return {};
  }
  if (isArray(obj)) {
    const result2 = Array.from(obj);
    if (obj.length > 0 && typeof obj[0] === "string" && Object.hasOwn(obj, "index")) {
      result2.index = obj.index;
      result2.input = obj.input;
    }
    return result2;
  }
  if (isTypedArray2(obj)) {
    const typedArray = obj;
    const Ctor = typedArray.constructor;
    return new Ctor(typedArray.buffer, typedArray.byteOffset, typedArray.length);
  }
  if (tag === arrayBufferTag) {
    return new ArrayBuffer(obj.byteLength);
  }
  if (tag === dataViewTag) {
    const dataView = obj;
    const buffer = dataView.buffer;
    const byteOffset = dataView.byteOffset;
    const byteLength = dataView.byteLength;
    const clonedBuffer = new ArrayBuffer(byteLength);
    const srcView = new Uint8Array(buffer, byteOffset, byteLength);
    const destView = new Uint8Array(clonedBuffer);
    destView.set(srcView);
    return new DataView(clonedBuffer);
  }
  if (tag === booleanTag || tag === numberTag || tag === stringTag) {
    const Ctor = obj.constructor;
    const clone3 = new Ctor(obj.valueOf());
    if (tag === stringTag) {
      cloneStringObjectProperties(clone3, obj);
    } else {
      copyOwnProperties(clone3, obj);
    }
    return clone3;
  }
  if (tag === dateTag) {
    return new Date(Number(obj));
  }
  if (tag === regexpTag) {
    const regExp = obj;
    const clone3 = new RegExp(regExp.source, regExp.flags);
    clone3.lastIndex = regExp.lastIndex;
    return clone3;
  }
  if (tag === symbolTag) {
    return Object(Symbol.prototype.valueOf.call(obj));
  }
  if (tag === mapTag) {
    const map = obj;
    const result2 = /* @__PURE__ */ new Map();
    map.forEach((obj2, key) => {
      result2.set(key, obj2);
    });
    return result2;
  }
  if (tag === setTag) {
    const set = obj;
    const result2 = /* @__PURE__ */ new Set();
    set.forEach((obj2) => {
      result2.add(obj2);
    });
    return result2;
  }
  if (tag === argumentsTag) {
    const args = obj;
    const result2 = {};
    copyOwnProperties(result2, args);
    result2.length = args.length;
    result2[Symbol.iterator] = args[Symbol.iterator];
    return result2;
  }
  const result = {};
  copyPrototype(result, obj);
  copyOwnProperties(result, obj);
  copySymbolProperties(result, obj);
  return result;
}
__name(clone, "clone");
function isCloneableObject2(object) {
  switch (getTag(object)) {
    case argumentsTag:
    case arrayTag:
    case arrayBufferTag:
    case dataViewTag:
    case booleanTag:
    case dateTag:
    case float32ArrayTag:
    case float64ArrayTag:
    case int8ArrayTag:
    case int16ArrayTag:
    case int32ArrayTag:
    case mapTag:
    case numberTag:
    case objectTag:
    case regexpTag:
    case setTag:
    case stringTag:
    case symbolTag:
    case uint8ArrayTag:
    case uint8ClampedArrayTag:
    case uint16ArrayTag:
    case uint32ArrayTag: {
      return true;
    }
    default: {
      return false;
    }
  }
}
__name(isCloneableObject2, "isCloneableObject");
function copyOwnProperties(target, source) {
  for (const key in source) {
    if (Object.hasOwn(source, key)) {
      target[key] = source[key];
    }
  }
}
__name(copyOwnProperties, "copyOwnProperties");
function copySymbolProperties(target, source) {
  const symbols = Object.getOwnPropertySymbols(source);
  for (let i = 0; i < symbols.length; i++) {
    const symbol = symbols[i];
    if (Object.prototype.propertyIsEnumerable.call(source, symbol)) {
      target[symbol] = source[symbol];
    }
  }
}
__name(copySymbolProperties, "copySymbolProperties");
function cloneStringObjectProperties(target, source) {
  const stringLength = source.valueOf().length;
  for (const key in source) {
    if (Object.hasOwn(source, key) && (Number.isNaN(Number(key)) || Number(key) >= stringLength)) {
      target[key] = source[key];
    }
  }
}
__name(cloneStringObjectProperties, "cloneStringObjectProperties");
function copyPrototype(target, source) {
  const proto = Object.getPrototypeOf(source);
  if (proto !== null) {
    const Ctor = source.constructor;
    if (typeof Ctor === "function") {
      Object.setPrototypeOf(target, proto);
    }
  }
}
__name(copyPrototype, "copyPrototype");

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/compat/predicate/isPlainObject.mjs
function isPlainObject(object) {
  if (typeof object !== "object") {
    return false;
  }
  if (object == null) {
    return false;
  }
  if (Object.getPrototypeOf(object) === null) {
    return true;
  }
  if (Object.prototype.toString.call(object) !== "[object Object]") {
    const tag = object[Symbol.toStringTag];
    if (tag == null) {
      return false;
    }
    const isTagReadonly = !Object.getOwnPropertyDescriptor(object, Symbol.toStringTag)?.writable;
    if (isTagReadonly) {
      return false;
    }
    return object.toString() === `[object ${tag}]`;
  }
  let proto = object;
  while (Object.getPrototypeOf(proto) !== null) {
    proto = Object.getPrototypeOf(proto);
  }
  return Object.getPrototypeOf(object) === proto;
}
__name(isPlainObject, "isPlainObject");

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/object/clone.mjs
function clone2(obj) {
  if (isPrimitive(obj)) {
    return obj;
  }
  if (Array.isArray(obj) || isTypedArray(obj) || obj instanceof ArrayBuffer || typeof SharedArrayBuffer !== "undefined" && obj instanceof SharedArrayBuffer) {
    return obj.slice(0);
  }
  const prototype = Object.getPrototypeOf(obj);
  if (prototype == null) {
    return Object.assign(Object.create(prototype), obj);
  }
  const Constructor = prototype.constructor;
  if (obj instanceof Date || obj instanceof Map || obj instanceof Set) {
    return new Constructor(obj);
  }
  if (obj instanceof RegExp) {
    const newRegExp = new Constructor(obj);
    newRegExp.lastIndex = obj.lastIndex;
    return newRegExp;
  }
  if (obj instanceof DataView) {
    return new Constructor(obj.buffer.slice(0));
  }
  if (obj instanceof Error) {
    let newError;
    if (obj instanceof AggregateError) {
      newError = new Constructor(obj.errors, obj.message, { cause: obj.cause });
    } else {
      newError = new Constructor(obj.message, { cause: obj.cause });
    }
    newError.stack = obj.stack;
    Object.assign(newError, obj);
    return newError;
  }
  if (typeof File !== "undefined" && obj instanceof File) {
    const newFile = new Constructor([obj], obj.name, { type: obj.type, lastModified: obj.lastModified });
    return newFile;
  }
  if (typeof obj === "object") {
    const newObject = Object.create(prototype);
    return Object.assign(newObject, obj);
  }
  return obj;
}
__name(clone2, "clone");

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/compat/object/mergeWith.mjs
function mergeWith(object, ...otherArgs) {
  const sources = otherArgs.slice(0, -1);
  const merge2 = otherArgs[otherArgs.length - 1];
  let result = object;
  for (let i = 0; i < sources.length; i++) {
    const source = sources[i];
    result = mergeWithDeep(result, source, merge2, /* @__PURE__ */ new Map());
  }
  return result;
}
__name(mergeWith, "mergeWith");
function mergeWithDeep(target, source, merge2, stack) {
  if (isPrimitive(target)) {
    target = Object(target);
  }
  if (source == null || typeof source !== "object") {
    return target;
  }
  if (stack.has(source)) {
    return clone2(stack.get(source));
  }
  stack.set(source, target);
  if (Array.isArray(source)) {
    source = source.slice();
    for (let i = 0; i < source.length; i++) {
      source[i] = source[i] ?? void 0;
    }
  }
  const sourceKeys = [...Object.keys(source), ...getSymbols(source)];
  for (let i = 0; i < sourceKeys.length; i++) {
    const key = sourceKeys[i];
    if (isUnsafeProperty(key)) {
      continue;
    }
    let sourceValue = source[key];
    let targetValue = target[key];
    if (isArguments(sourceValue)) {
      sourceValue = { ...sourceValue };
    }
    if (isArguments(targetValue)) {
      targetValue = { ...targetValue };
    }
    if (typeof Buffer !== "undefined" && Buffer.isBuffer(sourceValue)) {
      sourceValue = cloneDeep(sourceValue);
    }
    if (Array.isArray(sourceValue)) {
      if (Array.isArray(targetValue)) {
        const cloned = [];
        const targetKeys = Reflect.ownKeys(targetValue);
        for (let i2 = 0; i2 < targetKeys.length; i2++) {
          const targetKey = targetKeys[i2];
          cloned[targetKey] = targetValue[targetKey];
        }
        targetValue = cloned;
      } else if (isArrayLikeObject(targetValue)) {
        const cloned = [];
        for (let i2 = 0; i2 < targetValue.length; i2++) {
          cloned[i2] = targetValue[i2];
        }
        targetValue = cloned;
      } else {
        targetValue = [];
      }
    }
    const merged = merge2(targetValue, sourceValue, key, target, source, stack);
    if (merged !== void 0) {
      target[key] = merged;
    } else if (Array.isArray(sourceValue)) {
      target[key] = mergeWithDeep(targetValue, sourceValue, merge2, stack);
    } else if (isObjectLike(targetValue) && isObjectLike(sourceValue) && (isPlainObject(targetValue) || isPlainObject(sourceValue) || isTypedArray2(targetValue) || isTypedArray2(sourceValue))) {
      target[key] = mergeWithDeep(targetValue, sourceValue, merge2, stack);
    } else if (targetValue == null && isPlainObject(sourceValue)) {
      target[key] = mergeWithDeep({}, sourceValue, merge2, stack);
    } else if (targetValue == null && isTypedArray2(sourceValue)) {
      target[key] = cloneDeep(sourceValue);
    } else if (targetValue === void 0 || sourceValue !== void 0) {
      target[key] = sourceValue;
    }
  }
  return target;
}
__name(mergeWithDeep, "mergeWithDeep");

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/compat/object/merge.mjs
function merge(object, ...sources) {
  return mergeWith(object, ...sources, noop);
}
__name(merge, "merge");

// ../../node_modules/.pnpm/es-toolkit@1.45.1/node_modules/es-toolkit/dist/compat/predicate/isEmpty.mjs
function isEmpty(value) {
  if (value == null) {
    return true;
  }
  if (isArrayLike(value)) {
    if (typeof value.splice !== "function" && typeof value !== "string" && (typeof Buffer === "undefined" || !Buffer.isBuffer(value)) && !isTypedArray2(value) && !isArguments(value)) {
      return false;
    }
    return value.length === 0;
  }
  if (typeof value === "object") {
    if (value instanceof Map || value instanceof Set) {
      return value.size === 0;
    }
    const keys = Object.keys(value);
    if (isPrototype(value)) {
      return keys.filter((x) => x !== "constructor").length === 0;
    }
    return keys.length === 0;
  }
  return true;
}
__name(isEmpty, "isEmpty");

// src/utils.ts
var ZERO_WIDTH_SPACE = "\u200B";
var d3CurveTypes = {
  curveBasis: basis_default,
  curveBasisClosed: basisClosed_default,
  curveBasisOpen: basisOpen_default,
  curveBumpX: bumpX,
  curveBumpY: bumpY,
  curveBundle: bundle_default,
  curveCardinalClosed: cardinalClosed_default,
  curveCardinalOpen: cardinalOpen_default,
  curveCardinal: cardinal_default,
  curveCatmullRomClosed: catmullRomClosed_default,
  curveCatmullRomOpen: catmullRomOpen_default,
  curveCatmullRom: catmullRom_default,
  curveLinear: linear_default,
  curveLinearClosed: linearClosed_default,
  curveMonotoneX: monotoneX,
  curveMonotoneY: monotoneY,
  curveNatural: natural_default,
  curveStep: step_default,
  curveStepAfter: stepAfter,
  curveStepBefore: stepBefore
};
var directiveWithoutOpen = /\s*(?:(\w+)(?=:):|(\w+))\s*(?:(\w+)|((?:(?!}%{2}).|\r?\n)*))?\s*(?:}%{2})?/gi;
var detectInit = /* @__PURE__ */ __name(function(text, config) {
  const inits = detectDirective(text, /(?:init\b)|(?:initialize\b)/);
  let results = {};
  if (Array.isArray(inits)) {
    const args = inits.map((init) => init.args);
    sanitizeDirective(args);
    results = assignWithDepth_default(results, [...args]);
  } else {
    results = inits.args;
  }
  if (!results) {
    return;
  }
  let type = detectType(text, config);
  const prop = "config";
  if (results[prop] !== void 0) {
    if (type === "flowchart-v2") {
      type = "flowchart";
    }
    results[type] = results[prop];
    delete results[prop];
  }
  return results;
}, "detectInit");
var detectDirective = /* @__PURE__ */ __name(function(text, type = null) {
  try {
    const commentWithoutDirectives = new RegExp(
      `[%]{2}(?![{]${directiveWithoutOpen.source})(?=[}][%]{2}).*
`,
      "ig"
    );
    text = text.trim().replace(commentWithoutDirectives, "").replace(/'/gm, '"');
    log.debug(
      `Detecting diagram directive${type !== null ? " type:" + type : ""} based on the text:${text}`
    );
    let match;
    const result = [];
    while ((match = directiveRegex.exec(text)) !== null) {
      if (match.index === directiveRegex.lastIndex) {
        directiveRegex.lastIndex++;
      }
      if (match && !type || type && match[1]?.match(type) || type && match[2]?.match(type)) {
        const type2 = match[1] ? match[1] : match[2];
        const args = match[3] ? match[3].trim() : match[4] ? JSON.parse(match[4].trim()) : null;
        result.push({ type: type2, args });
      }
    }
    if (result.length === 0) {
      return { type: text, args: null };
    }
    return result.length === 1 ? result[0] : result;
  } catch (error) {
    log.error(
      `ERROR: ${error.message} - Unable to parse directive type: '${type}' based on the text: '${text}'`
    );
    return { type: void 0, args: null };
  }
}, "detectDirective");
var removeDirectives = /* @__PURE__ */ __name(function(text) {
  return text.replace(directiveRegex, "");
}, "removeDirectives");
var isSubstringInArray = /* @__PURE__ */ __name(function(str, arr) {
  for (const [i, element] of arr.entries()) {
    if (element.match(str)) {
      return i;
    }
  }
  return -1;
}, "isSubstringInArray");
function interpolateToCurve(interpolate, defaultCurve) {
  if (!interpolate) {
    return defaultCurve;
  }
  const curveName = `curve${interpolate.charAt(0).toUpperCase() + interpolate.slice(1)}`;
  return d3CurveTypes[curveName] ?? defaultCurve;
}
__name(interpolateToCurve, "interpolateToCurve");
function formatUrl(linkStr, config) {
  const url = linkStr.trim();
  if (!url) {
    return void 0;
  }
  if (config.securityLevel !== "loose") {
    return (0, import_sanitize_url.sanitizeUrl)(url);
  }
  return url;
}
__name(formatUrl, "formatUrl");
var runFunc = /* @__PURE__ */ __name((functionName, ...params) => {
  const arrPaths = functionName.split(".");
  const len = arrPaths.length - 1;
  const fnName = arrPaths[len];
  let obj = window;
  for (let i = 0; i < len; i++) {
    obj = obj[arrPaths[i]];
    if (!obj) {
      log.error(`Function name: ${functionName} not found in window`);
      return;
    }
  }
  obj[fnName](...params);
}, "runFunc");
function distance(p1, p2) {
  if (!p1 || !p2) {
    return 0;
  }
  return Math.sqrt(Math.pow(p2.x - p1.x, 2) + Math.pow(p2.y - p1.y, 2));
}
__name(distance, "distance");
function traverseEdge(points) {
  let prevPoint;
  let totalDistance = 0;
  points.forEach((point) => {
    totalDistance += distance(point, prevPoint);
    prevPoint = point;
  });
  const remainingDistance = totalDistance / 2;
  return calculatePoint(points, remainingDistance);
}
__name(traverseEdge, "traverseEdge");
function calcLabelPosition(points) {
  if (points.length === 1) {
    return points[0];
  }
  return traverseEdge(points);
}
__name(calcLabelPosition, "calcLabelPosition");
var roundNumber = /* @__PURE__ */ __name((num, precision = 2) => {
  const factor = Math.pow(10, precision);
  return Math.round(num * factor) / factor;
}, "roundNumber");
var calculatePoint = /* @__PURE__ */ __name((points, distanceToTraverse) => {
  let prevPoint = void 0;
  let remainingDistance = distanceToTraverse;
  for (const point of points) {
    if (prevPoint) {
      const vectorDistance = distance(point, prevPoint);
      if (vectorDistance === 0) {
        return prevPoint;
      }
      if (vectorDistance < remainingDistance) {
        remainingDistance -= vectorDistance;
      } else {
        const distanceRatio = remainingDistance / vectorDistance;
        if (distanceRatio <= 0) {
          return prevPoint;
        }
        if (distanceRatio >= 1) {
          return { x: point.x, y: point.y };
        }
        if (distanceRatio > 0 && distanceRatio < 1) {
          return {
            x: roundNumber((1 - distanceRatio) * prevPoint.x + distanceRatio * point.x, 5),
            y: roundNumber((1 - distanceRatio) * prevPoint.y + distanceRatio * point.y, 5)
          };
        }
      }
    }
    prevPoint = point;
  }
  throw new Error("Could not find a suitable point for the given distance");
}, "calculatePoint");
var calcCardinalityPosition = /* @__PURE__ */ __name((isRelationTypePresent, points, initialPosition) => {
  log.info(`our points ${JSON.stringify(points)}`);
  if (points[0] !== initialPosition) {
    points = points.reverse();
  }
  const distanceToCardinalityPoint = 25;
  const center = calculatePoint(points, distanceToCardinalityPoint);
  const d = isRelationTypePresent ? 10 : 5;
  const angle = Math.atan2(points[0].y - center.y, points[0].x - center.x);
  const cardinalityPosition = { x: 0, y: 0 };
  cardinalityPosition.x = Math.sin(angle) * d + (points[0].x + center.x) / 2;
  cardinalityPosition.y = -Math.cos(angle) * d + (points[0].y + center.y) / 2;
  return cardinalityPosition;
}, "calcCardinalityPosition");
function calcTerminalLabelPosition(terminalMarkerSize, position, _points) {
  const points = structuredClone(_points);
  log.info("our points", points);
  if (position !== "start_left" && position !== "start_right") {
    points.reverse();
  }
  const distanceToCardinalityPoint = 25 + terminalMarkerSize;
  const center = calculatePoint(points, distanceToCardinalityPoint);
  const d = 10 + terminalMarkerSize * 0.5;
  const angle = Math.atan2(points[0].y - center.y, points[0].x - center.x);
  const cardinalityPosition = { x: 0, y: 0 };
  if (position === "start_left") {
    cardinalityPosition.x = Math.sin(angle + Math.PI) * d + (points[0].x + center.x) / 2;
    cardinalityPosition.y = -Math.cos(angle + Math.PI) * d + (points[0].y + center.y) / 2;
  } else if (position === "end_right") {
    cardinalityPosition.x = Math.sin(angle - Math.PI) * d + (points[0].x + center.x) / 2 - 5;
    cardinalityPosition.y = -Math.cos(angle - Math.PI) * d + (points[0].y + center.y) / 2 - 5;
  } else if (position === "end_left") {
    cardinalityPosition.x = Math.sin(angle) * d + (points[0].x + center.x) / 2 - 5;
    cardinalityPosition.y = -Math.cos(angle) * d + (points[0].y + center.y) / 2 - 5;
  } else {
    cardinalityPosition.x = Math.sin(angle) * d + (points[0].x + center.x) / 2;
    cardinalityPosition.y = -Math.cos(angle) * d + (points[0].y + center.y) / 2;
  }
  return cardinalityPosition;
}
__name(calcTerminalLabelPosition, "calcTerminalLabelPosition");
function getStylesFromArray(arr) {
  let style = "";
  let labelStyle = "";
  for (const element of arr) {
    if (element !== void 0) {
      if (element.startsWith("color:") || element.startsWith("text-align:")) {
        labelStyle = labelStyle + element + ";";
      } else {
        style = style + element + ";";
      }
    }
  }
  return { style, labelStyle };
}
__name(getStylesFromArray, "getStylesFromArray");
var cnt = 0;
var generateId = /* @__PURE__ */ __name(() => {
  cnt++;
  return "id-" + Math.random().toString(36).substr(2, 12) + "-" + cnt;
}, "generateId");
function makeRandomHex(length) {
  let result = "";
  const characters = "0123456789abcdef";
  const charactersLength = characters.length;
  for (let i = 0; i < length; i++) {
    result += characters.charAt(Math.floor(Math.random() * charactersLength));
  }
  return result;
}
__name(makeRandomHex, "makeRandomHex");
var random = /* @__PURE__ */ __name((options) => {
  return makeRandomHex(options.length);
}, "random");
var getTextObj = /* @__PURE__ */ __name(function() {
  return {
    x: 0,
    y: 0,
    fill: void 0,
    anchor: "start",
    style: "#666",
    width: 100,
    height: 100,
    textMargin: 0,
    rx: 0,
    ry: 0,
    valign: void 0,
    text: ""
  };
}, "getTextObj");
var drawSimpleText = /* @__PURE__ */ __name(function(elem, textData) {
  const nText = textData.text.replace(common_default.lineBreakRegex, " ");
  const [, _fontSizePx] = parseFontSize(textData.fontSize);
  const textElem = elem.append("text");
  textElem.attr("x", textData.x);
  textElem.attr("y", textData.y);
  textElem.style("text-anchor", textData.anchor);
  textElem.style("font-family", textData.fontFamily);
  textElem.style("font-size", _fontSizePx);
  textElem.style("font-weight", textData.fontWeight);
  textElem.attr("fill", textData.fill);
  if (textData.class !== void 0) {
    textElem.attr("class", textData.class);
  }
  const span = textElem.append("tspan");
  span.attr("x", textData.x + textData.textMargin * 2);
  span.attr("fill", textData.fill);
  span.text(nText);
  return textElem;
}, "drawSimpleText");
var wrapLabel = memoize(
  (label, maxWidth, config) => {
    if (!label) {
      return label;
    }
    config = Object.assign(
      { fontSize: 12, fontWeight: 400, fontFamily: "Arial", joinWith: "<br/>" },
      config
    );
    if (common_default.lineBreakRegex.test(label)) {
      return label;
    }
    const words = label.split(" ").filter(Boolean);
    const completedLines = [];
    let nextLine = "";
    words.forEach((word, index) => {
      const wordLength = calculateTextWidth(`${word} `, config);
      const nextLineLength = calculateTextWidth(nextLine, config);
      if (wordLength > maxWidth) {
        const { hyphenatedStrings, remainingWord } = breakString(word, maxWidth, "-", config);
        completedLines.push(nextLine, ...hyphenatedStrings);
        nextLine = remainingWord;
      } else if (nextLineLength + wordLength >= maxWidth) {
        completedLines.push(nextLine);
        nextLine = word;
      } else {
        nextLine = [nextLine, word].filter(Boolean).join(" ");
      }
      const currentWord = index + 1;
      const isLastWord = currentWord === words.length;
      if (isLastWord) {
        completedLines.push(nextLine);
      }
    });
    return completedLines.filter((line) => line !== "").join(config.joinWith);
  },
  (label, maxWidth, config) => `${label}${maxWidth}${config.fontSize}${config.fontWeight}${config.fontFamily}${config.joinWith}`
);
var breakString = memoize(
  (word, maxWidth, hyphenCharacter = "-", config) => {
    config = Object.assign(
      { fontSize: 12, fontWeight: 400, fontFamily: "Arial", margin: 0 },
      config
    );
    const characters = [...word];
    const lines = [];
    let currentLine = "";
    characters.forEach((character, index) => {
      const nextLine = `${currentLine}${character}`;
      const lineWidth = calculateTextWidth(nextLine, config);
      if (lineWidth >= maxWidth) {
        const currentCharacter = index + 1;
        const isLastLine = characters.length === currentCharacter;
        const hyphenatedNextLine = `${nextLine}${hyphenCharacter}`;
        lines.push(isLastLine ? nextLine : hyphenatedNextLine);
        currentLine = "";
      } else {
        currentLine = nextLine;
      }
    });
    return { hyphenatedStrings: lines, remainingWord: currentLine };
  },
  (word, maxWidth, hyphenCharacter = "-", config) => `${word}${maxWidth}${hyphenCharacter}${config.fontSize}${config.fontWeight}${config.fontFamily}`
);
function calculateTextHeight(text, config) {
  return calculateTextDimensions(text, config).height;
}
__name(calculateTextHeight, "calculateTextHeight");
function calculateTextWidth(text, config) {
  return calculateTextDimensions(text, config).width;
}
__name(calculateTextWidth, "calculateTextWidth");
var calculateTextDimensions = memoize(
  (text, config) => {
    const { fontSize = 12, fontFamily = "Arial", fontWeight = 400 } = config;
    if (!text) {
      return { width: 0, height: 0 };
    }
    const [, _fontSizePx] = parseFontSize(fontSize);
    const fontFamilies = ["sans-serif", fontFamily];
    const lines = text.split(common_default.lineBreakRegex);
    const dims = [];
    const body = select_default("body");
    if (!body.remove) {
      return { width: 0, height: 0, lineHeight: 0 };
    }
    const g = body.append("svg");
    for (const fontFamily2 of fontFamilies) {
      let cHeight = 0;
      const dim = { width: 0, height: 0, lineHeight: 0 };
      for (const line of lines) {
        const textObj = getTextObj();
        textObj.text = line || ZERO_WIDTH_SPACE;
        const textElem = drawSimpleText(g, textObj).style("font-size", _fontSizePx).style("font-weight", fontWeight).style("font-family", fontFamily2);
        const bBox = (textElem._groups || textElem)[0][0].getBBox();
        if (bBox.width === 0 && bBox.height === 0) {
          throw new Error("svg element not in render tree");
        }
        dim.width = Math.round(Math.max(dim.width, bBox.width));
        cHeight = Math.round(bBox.height);
        dim.height += cHeight;
        dim.lineHeight = Math.round(Math.max(dim.lineHeight, cHeight));
      }
      dims.push(dim);
    }
    g.remove();
    const index = isNaN(dims[1].height) || isNaN(dims[1].width) || isNaN(dims[1].lineHeight) || dims[0].height > dims[1].height && dims[0].width > dims[1].width && dims[0].lineHeight > dims[1].lineHeight ? 0 : 1;
    return dims[index];
  },
  (text, config) => `${text}${config.fontSize}${config.fontWeight}${config.fontFamily}`
);
var InitIDGenerator = class {
  constructor(deterministic = false, seed) {
    this.count = 0;
    this.count = seed ? seed.length : 0;
    this.next = deterministic ? () => this.count++ : () => Date.now();
  }
  static {
    __name(this, "InitIDGenerator");
  }
};
var decoder;
var entityDecode = /* @__PURE__ */ __name(function(html) {
  decoder = decoder || document.createElement("div");
  html = escape(html).replace(/%26/g, "&").replace(/%23/g, "#").replace(/%3B/g, ";");
  decoder.innerHTML = html;
  return unescape(decoder.textContent);
}, "entityDecode");
function isDetailedError(error) {
  return "str" in error;
}
__name(isDetailedError, "isDetailedError");
var insertTitle = /* @__PURE__ */ __name((parent, cssClass, titleTopMargin, title) => {
  if (!title) {
    return;
  }
  const bounds = parent.node()?.getBBox();
  if (!bounds) {
    return;
  }
  parent.append("text").text(title).attr("text-anchor", "middle").attr("x", bounds.x + bounds.width / 2).attr("y", -titleTopMargin).attr("class", cssClass);
}, "insertTitle");
var parseFontSize = /* @__PURE__ */ __name((fontSize) => {
  if (typeof fontSize === "number") {
    return [fontSize, fontSize + "px"];
  }
  const fontSizeNumber = parseInt(fontSize ?? "", 10);
  if (Number.isNaN(fontSizeNumber)) {
    return [void 0, void 0];
  } else if (fontSize === String(fontSizeNumber)) {
    return [fontSizeNumber, fontSize + "px"];
  } else {
    return [fontSizeNumber, fontSize];
  }
}, "parseFontSize");
function cleanAndMerge(defaultData, data) {
  return merge({}, defaultData, data);
}
__name(cleanAndMerge, "cleanAndMerge");
var utils_default = {
  assignWithDepth: assignWithDepth_default,
  wrapLabel,
  calculateTextHeight,
  calculateTextWidth,
  calculateTextDimensions,
  cleanAndMerge,
  detectInit,
  detectDirective,
  isSubstringInArray,
  interpolateToCurve,
  calcLabelPosition,
  calcCardinalityPosition,
  calcTerminalLabelPosition,
  formatUrl,
  getStylesFromArray,
  generateId,
  random,
  runFunc,
  entityDecode,
  insertTitle,
  isLabelCoordinateInPath,
  parseFontSize,
  InitIDGenerator
};
var encodeEntities = /* @__PURE__ */ __name(function(text) {
  let txt = text;
  txt = txt.replace(/style.*:\S*#.*;/g, function(s) {
    return s.substring(0, s.length - 1);
  });
  txt = txt.replace(/classDef.*:\S*#.*;/g, function(s) {
    return s.substring(0, s.length - 1);
  });
  txt = txt.replace(/#\w+;/g, function(s) {
    const innerTxt = s.substring(1, s.length - 1);
    const isInt = /^\+?\d+$/.test(innerTxt);
    if (isInt) {
      return "\uFB02\xB0\xB0" + innerTxt + "\xB6\xDF";
    } else {
      return "\uFB02\xB0" + innerTxt + "\xB6\xDF";
    }
  });
  return txt;
}, "encodeEntities");
var decodeEntities = /* @__PURE__ */ __name(function(text) {
  return text.replace(/ﬂ°°/g, "&#").replace(/ﬂ°/g, "&").replace(/¶ß/g, ";");
}, "decodeEntities");
var getEdgeId = /* @__PURE__ */ __name((from, to, {
  counter = 0,
  prefix,
  suffix
}, id) => {
  if (id) {
    return id;
  }
  return `${prefix ? `${prefix}_` : ""}${from}_${to}_${counter}${suffix ? `_${suffix}` : ""}`;
}, "getEdgeId");
function handleUndefinedAttr(attrValue) {
  return attrValue ?? null;
}
__name(handleUndefinedAttr, "handleUndefinedAttr");
function isLabelCoordinateInPath(point, dAttr) {
  const roundedX = Math.round(point.x);
  const roundedY = Math.round(point.y);
  const sanitizedD = dAttr.replace(
    /(\d+\.\d+)/g,
    (match) => Math.round(parseFloat(match)).toString()
  );
  return sanitizedD.includes(roundedX.toString()) || sanitizedD.includes(roundedY.toString());
}
__name(isLabelCoordinateInPath, "isLabelCoordinateInPath");

export {
  clone,
  isEmpty,
  ZERO_WIDTH_SPACE,
  removeDirectives,
  interpolateToCurve,
  getStylesFromArray,
  generateId,
  random,
  wrapLabel,
  calculateTextHeight,
  calculateTextWidth,
  calculateTextDimensions,
  isDetailedError,
  parseFontSize,
  cleanAndMerge,
  utils_default,
  encodeEntities,
  decodeEntities,
  getEdgeId,
  handleUndefinedAttr
};
