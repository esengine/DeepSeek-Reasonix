const {
  entries,
  setPrototypeOf,
  isFrozen,
  getPrototypeOf,
  getOwnPropertyDescriptor,
} = Object;

let { freeze, seal, create } = Object; // eslint-disable-line import/no-mutable-exports
let { apply, construct } = typeof Reflect !== 'undefined' && Reflect;

if (!freeze) {
  freeze = function <T>(x: T): T {
    return x;
  };
}

if (!seal) {
  seal = function <T>(x: T): T {
    return x;
  };
}

if (!apply) {
  apply = function <T>(
    func: (thisArg: any, ...args: any[]) => T,
    thisArg: any,
    ...args: any[]
  ): T {
    return func.apply(thisArg, args);
  };
}

if (!construct) {
  construct = function <T>(Func: new (...args: any[]) => T, ...args: any[]): T {
    return new Func(...args);
  };
}

const arrayForEach = unapply(Array.prototype.forEach);
const arrayIndexOf = unapply(Array.prototype.indexOf);
const arrayLastIndexOf = unapply(Array.prototype.lastIndexOf);
const arrayPop = unapply(Array.prototype.pop);
const arrayPush = unapply(Array.prototype.push);
const arraySlice = unapply(Array.prototype.slice);
const arraySplice = unapply(Array.prototype.splice);
const arrayIsArray = Array.isArray;

const stringToLowerCase = unapply(String.prototype.toLowerCase);
const stringToString = unapply(String.prototype.toString);
const stringMatch = unapply(String.prototype.match);
const stringReplace = unapply(String.prototype.replace);
const stringIndexOf = unapply(String.prototype.indexOf);
const stringTrim = unapply(String.prototype.trim);

const numberToString = unapply(Number.prototype.toString);
const booleanToString = unapply(Boolean.prototype.toString);
const bigintToString =
  typeof BigInt === 'undefined' ? null : unapply(BigInt.prototype.toString);
const symbolToString =
  typeof Symbol === 'undefined' ? null : unapply(Symbol.prototype.toString);

const objectHasOwnProperty = unapply(Object.prototype.hasOwnProperty);
const objectToString = unapply(Object.prototype.toString);

const regExpTest = unapply(RegExp.prototype.test);

const typeErrorCreate = unconstruct(TypeError);

/**
 * Creates a new function that calls the given function with a specified thisArg and arguments.
 *
 * @param func - The function to be wrapped and called.
 * @returns A new function that calls the given function with a specified thisArg and arguments.
 */
function unapply<T>(
  func: (thisArg: any, ...args: any[]) => T
): (thisArg: any, ...args: any[]) => T {
  return (thisArg: any, ...args: any[]): T => {
    if (thisArg instanceof RegExp) {
      thisArg.lastIndex = 0;
    }

    return apply(func, thisArg, args);
  };
}

/**
 * Creates a new function that constructs an instance of the given constructor function with the provided arguments.
 *
 * @param func - The constructor function to be wrapped and called.
 * @returns A new function that constructs an instance of the given constructor function with the provided arguments.
 */
function unconstruct<T>(
  Func: new (...args: any[]) => T
): (...args: any[]) => T {
  return (...args: any[]): T => construct(Func, args);
}

/**
 * Add properties to a lookup table
 *
 * @param set - The set to which elements will be added.
 * @param array - The array containing elements to be added to the set.
 * @param transformCaseFunc - An optional function to transform the case of each element before adding to the set.
 * @returns The modified set with added elements.
 */
function addToSet(
  set: Record<string, boolean>,
  array: readonly unknown[],
  transformCaseFunc: ReturnType<typeof unapply<string>> = stringToLowerCase
): Record<string, boolean> {
  if (setPrototypeOf) {
    // Make 'in' and truthy checks like Boolean(set.constructor)
    // independent of any properties defined on Object.prototype.
    // Prevent prototype setters from intercepting set as a this value.
    setPrototypeOf(set, null);
  }

  if (!arrayIsArray(array)) {
    return set;
  }

  let l = array.length;
  while (l--) {
    let element = array[l];

    if (typeof element === 'string') {
      const lcElement = transformCaseFunc(element);

      if (lcElement !== element) {
        // Config presets (e.g. tags.js, attrs.js) are immutable.
        if (!isFrozen(array)) {
          (array as unknown[])[l] = lcElement;
        }

        element = lcElement;
      }
    }

    set[element as string] = true;
  }

  return set;
}

/**
 * Clean up an array to harden against CSPP
 *
 * @param array - The array to be cleaned.
 * @returns The cleaned version of the array
 */
function cleanArray<T>(array: T[]): Array<T | null> {
  for (let index = 0; index < array.length; index++) {
    const isPropertyExist = objectHasOwnProperty(array, index);

    if (!isPropertyExist) {
      array[index] = null;
    }
  }

  return array;
}

/**
 * Shallow clone an object
 *
 * @param object - The object to be cloned.
 * @returns A new object that copies the original.
 */
function clone<T extends Record<string, any>>(object: T): T {
  const newObject = create(null);

  for (const [property, value] of entries(object)) {
    const isPropertyExist = objectHasOwnProperty(object, property);

    if (isPropertyExist) {
      if (arrayIsArray(value)) {
        newObject[property] = cleanArray(value);
      } else if (
        value &&
        typeof value === 'object' &&
        value.constructor === Object
      ) {
        newObject[property] = clone(value);
      } else {
        newObject[property] = value;
      }
    }
  }

  return newObject;
}

/**
 * Convert non-node values into strings without depending on direct property access.
 *
 * @param value - The value to stringify.
 * @returns A string representation of the provided value.
 */
function stringifyValue(value: unknown): string {
  switch (typeof value) {
    case 'string': {
      return value;
    }

    case 'number': {
      return numberToString(value);
    }

    case 'boolean': {
      return booleanToString(value);
    }

    case 'bigint': {
      return bigintToString ? bigintToString(value) : '0';
    }

    case 'symbol': {
      return symbolToString ? symbolToString(value) : 'Symbol()';
    }

    case 'undefined': {
      return objectToString(value);
    }

    case 'function':
    case 'object': {
      if (value === null) {
        return objectToString(value);
      }

      const valueAsRecord = value as Record<string, any>;
      const valueToString = lookupGetter(valueAsRecord, 'toString');

      if (typeof valueToString === 'function') {
        const stringified = valueToString(valueAsRecord);

        return typeof stringified === 'string'
          ? stringified
          : objectToString(stringified);
      }

      return objectToString(value);
    }

    default: {
      return objectToString(value);
    }
  }
}

/**
 * This method automatically checks if the prop is function or getter and behaves accordingly.
 *
 * @param object - The object to look up the getter function in its prototype chain.
 * @param prop - The property name for which to find the getter function.
 * @returns The getter function found in the prototype chain or a fallback function.
 */
function lookupGetter<T extends Record<string, any>>(
  object: T,
  prop: string
): ReturnType<typeof unapply<any>> | (() => null) {
  while (object !== null) {
    const desc = getOwnPropertyDescriptor(object, prop);

    if (desc) {
      if (desc.get) {
        return unapply(desc.get);
      }

      if (typeof desc.value === 'function') {
        return unapply(desc.value);
      }
    }

    object = getPrototypeOf(object);
  }

  function fallbackValue(): null {
    return null;
  }

  return fallbackValue;
}

function isRegex(value: unknown): value is RegExp {
  try {
    regExpTest(value as RegExp, '');
    return true;
  } catch {
    return false;
  }
}

export {
  // Array
  arrayForEach,
  arrayIndexOf,
  arrayIsArray,
  arrayLastIndexOf,
  arrayPop,
  arrayPush,
  arraySlice,
  arraySplice,
  // Object
  entries,
  freeze,
  getPrototypeOf,
  getOwnPropertyDescriptor,
  isFrozen,
  setPrototypeOf,
  seal,
  clone,
  create,
  objectHasOwnProperty,
  objectToString,
  // RegExp
  regExpTest,
  isRegex,
  // String
  stringIndexOf,
  stringMatch,
  stringReplace,
  stringToLowerCase,
  stringToString,
  stringTrim,
  // Other conversion
  stringifyValue,
  // Errors
  typeErrorCreate,
  // Other
  lookupGetter,
  addToSet,
  // Reflect
  unapply,
  unconstruct,
};
