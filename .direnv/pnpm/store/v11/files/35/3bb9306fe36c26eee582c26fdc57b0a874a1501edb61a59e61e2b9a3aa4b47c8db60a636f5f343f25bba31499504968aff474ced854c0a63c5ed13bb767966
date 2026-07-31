declare namespace gsap.utils {

  interface DistributeConfig {
    amount?: number;
    axis?: "x" | "y";
    base?: number;
    each?: number;
    ease?: string | EaseFunction;
    from?: "start" | "center" | "end" | "edges" | "random" | number | [number, number];
    grid?: "auto" | [number, number];
  }

  interface SnapNumberConfig {
    increment?: number;
    values?: number[];
    radius: number;
  }

  interface SnapPoint2DConfig {
    values: Point2D[];
    radius: number;
  }

  interface SelectorFunc {
    <K extends keyof HTMLElementTagNameMap>(selectorText: string): Array<HTMLElementTagNameMap[K]>;
    <K extends keyof SVGElementTagNameMap>(selectorText: string): Array<SVGElementTagNameMap[K]>;
    <E extends Element = Element>(selectorText: string): Array<E>;
  }

  /**
   * Prefixes the provided CSS property if necessary. Returns null if the property isn't supported at all.
   * 
   * ```js
   * // The following may return "filter", "WebkitFilter", or "MozFilter" depending on the browser
   * let filterProperty = gsap.utils.checkPrefix("filter");
   * ```
   *
   * @param {string} property
   * @returns {string | null} The appropriately prefixed property 
   * @memberof gsap.utils
   */
  function checkPrefix(property: string): string;

  /**
   * Clamps a number between a given minimum and maximum. 
   * 
   * ```js
   * gsap.utils.clamp(0, 100, 105); // returns 100
   * 
   * const clamper = gsap.utils.clamp(0, 100); // no value = reusable function
   * console.log(clamper(105)); // returns 100
   * ```
   *
   * @param {number} minimum
   * @param {number} maximum
   * @param {number} [valueToClamp]
   * @returns {number | Function} The clamped number or function to clamp to given range
   * @memberof gsap.utils
   */
  function clamp(minimum: number, maximum: number, valueToClamp: number): number;
  function clamp(minimum: number, maximum: number): (valueToClamp: number) => number;

  /**
   * Returns a function to distribute an array of values based on the inputs that you give it.
   * 
   * ```js
   * gsap.utils.distribute({
   *   base: 50,
   *   amount: 100,
   *   from: "center",
   *   grid: "auto",
   *   axis: "y",
   *   ease: "power1.inOut"
   * });
   * ```
   *
   * @param {DistributeConfig | NumberValue} config
   * @returns {FunctionBasedValue<number>} The clamped number or function to clamp to given range
   * @memberof gsap.utils
   */
  function distribute(config: DistributeConfig | NumberValue): FunctionBasedValue<number>;

  /**
   * Returns unit of a given string where the number comes first, then the unit.
   * 
   * ```js
   * gsap.utils.getUnit("50%"); // "%"
   * ```
   *
   * @param {string} value
   * @returns {string} The unit
   * @memberof gsap.utils
   */
  function getUnit(value: string): string;
  
  /**
   * Linearly interpolates between any two values of a similar type.
   * 
   * ```js
   * gsap.utils.interpolate(0, 500, 0.5); // 250
   * 
   * const interp = gsap.utils.interpolate(0, 100); // no value = reusable function
   * console.log( interp(0.5) ); // 50
   * ```
   *
   * @param {T} startValue
   * @param {T} endValue
   * @param {number} [number]
   * @returns {T | Function<number>} The interpolated value or interpolate function
   * @memberof gsap.utils
   */
  function interpolate<T>(startValue: T, endValue: T, progress: number): T;
  function interpolate<T>(startValue: T, endValue: T): (progress: number) => T;
  /**
   * Linearly interpolates between any two values of a similar type.
   * 
   * ```js
   * gsap.utils.interpolate([100, 50, 500], 0.5); // 50
   * 
   * c interp = gsap.utils.interpolate([100, 50, 500]); // no value = reusable function
   * console.log( interp(0.5) ); // 50
   * ```
   *
   * @param {T[]} array
   * @param {number} progress
   * @returns {T | Function} The interpolated value or interpolate function
   * @memberof gsap.utils
   */
  function interpolate<T>(array: T[], progress: number): T;
  function interpolate<T>(array: T[]): (progress: number) => T;
  
  /**
   * Maps a number's relative placement within one range to the equivalent position in another range.
   * 
   * ```js
   * gsap.utils.mapRange(-10, 10, 100, 200, 0); // 150
   * 
   * const mapper = gsap.utils.mapRange(0, 100, 0, 250); // no value = reusable function
   * console.log( mapper(50) ); // 125
   * ```
   *
   * @param {number} inMin
   * @param {number} inMax
   * @param {number} outMin
   * @param {number} outMax
   * @param {number} [value]
   * @returns {number | Function} The mapped value or map function
   * @memberof gsap.utils
   */
  function mapRange(inMin: number, inMax: number, outMin: number, outMax: number, value: number): number;
  function mapRange(inMin: number, inMax: number, outMin: number, outMax: number): (value: number) => number;
  
  /**
   * Maps a value within a provided range to the corresponding position in the range between 0 and 1.
   * 
   * ```js
   * gsap.utils.normalize(-10, 10, 0); // 0.5
   * 
   * const clamper = gsap.utils.normalize(0, 100); // no value = reusable function
   * console.log( clamper(50) ); // 0.5
   * ```
   *
   * @param {number} inMin
   * @param {number} inMax
   * @param {number} [value]
   * @returns {number | Function} The normalized value or normalizer function
   * @memberof gsap.utils
   */
  function normalize(inMin: number, inMax: number, value: number): number;
  function normalize(inMin: number, inMax: number): (value: number) => number;
  
  /**
   * Strings together multiple function calls, passing the result from one to the next. 
   * You can pass in as many function references as you'd like!
   * 
   * ```js
   * const transfrom = gsap.utils.pipe(func1, func2, func3); // reusable function
   * const output = transform(input);
   * ```
   *
   * @param {Function} ab
   * @param {Function} bc
   * @param {Function} [cd]
   * @returns {Function} The function that pipes values from function to function given
   * @memberof gsap.utils
   */
  function pipe<A extends Array<unknown>, B>(
    ab: (...a: A) => B
  ): (...a: A) => B;
  function pipe<A extends Array<unknown>, B, C>(
    ab: (...a: A) => B, 
    bc: (b: B) => C
  ): (...a: A) => C
  function pipe<A extends Array<unknown>, B, C, D>(
    ab: (...a: A) => B,
    bc: (b: B) => C,
    cd: (c: C) => D
  ): (...a: A) => D;
  function pipe<A extends Array<unknown>, B, C, D, E>(
    ab: (...a: A) => B,
    bc: (b: B) => C,
    cd: (c: C) => D,
    de: (d: D) => E
  ): (...a: A) => E;
  function pipe<A extends Array<unknown>, B, C, D, E, F>(
    ab: (...a: A) => B,
    bc: (b: B) => C,
    cd: (c: C) => D,
    de: (d: D) => E,
    ef: (e: E) => F
  ): (...a: A) => F;
  function pipe<A extends Array<unknown>, B, C, D, E, F, G>(
    ab: (...a: A) => B,
    bc: (b: B) => C,
    cd: (c: C) => D,
    de: (d: D) => E,
    ef: (e: E) => F,
    fg: (f: F) => G
  ): (...a: A) => G;
  function pipe<A extends Array<unknown>, B, C, D, E, F, G, H>(
    ab: (...a: A) => B,
    bc: (b: B) => C,
    cd: (c: C) => D,
    de: (d: D) => E,
    ef: (e: E) => F,
    fg: (f: F) => G,
    gh: (g: G) => H
  ): (...a: A) => H;
  function pipe<A extends Array<unknown>, B, C, D, E, F, G, H, I>(
    ab: (...a: A) => B,
    bc: (b: B) => C,
    cd: (c: C) => D,
    de: (d: D) => E,
    ef: (e: E) => F,
    fg: (f: F) => G,
    gh: (g: G) => H,
    hi: (h: H) => I
  ): (...a: A) => I;
  function pipe<A extends Array<unknown>, B, C, D, E, F, G, H, I, J>(
    ab: (...a: A) => B,
    bc: (b: B) => C,
    cd: (c: C) => D,
    de: (d: D) => E,
    ef: (e: E) => F,
    fg: (f: F) => G,
    gh: (g: G) => H,
    hi: (h: H) => I,
    ij: (i: I) => J
  ): (...a: A) => J;
  
  /**
   * Get a random number within a range, optionally rounding to an increment you provide.
   * 
   * ```js
   * gsap.utils.random(-100, 100);
   * gsap.utils.random(0, 500, 5); // snapped to the nearest value of 5
   *
   * const random = gsap.utils.random(-200, 500, 10, true); // reusable function
   * console.log( random() ); 
   * ```
   *
   * @param {number} minValue
   * @param {number} maxValue
   * @param {number} [snapIncrement]
   * @param {boolean} [returnFunction]
   * @returns {number | Function} The random number or random number generator function
   * @memberof gsap.utils
   */
  function random(minValue: number, maxValue: number, snapIncrement?: number): number;
  function random<T extends boolean>(minValue: number, maxValue: number, returnFunction?: T): T extends true ? () => number : number;
  function random<T extends boolean>(minValue: number, maxValue: number, snapIncrement: number, returnFunction?: T): T extends true ? () => number : number;
  /**
   * Get a random random element in an array.
   * 
   * ```js
   * gsap.utils.random(["red", "blue", "green"]); //"red", "blue", or "green"
   *
   * const random = gsap.utils.random([0, 100, 200], true);
   * console.log( random() ); // 0, 100, or 200 (randomly selected)
   * ```
   *
   * @param {T[]} array
   * @param {boolean} [returnFunction]
   * @returns {number | Function} The random number or random number generator function
   * @memberof gsap.utils
   */
  function random<T>(array: T[]): T;
  function random<T, U extends boolean>(array: T[], returnFunction?: U): U extends true ? () => T : T;

  /**
   * Returns a selector function that is scoped to a particular Element.
   *
   * ```js
   * const q = gsap.utils.selector("#id");
   * const q = gsap.utils.selector(myElement);
   * gsap.to(q(".class"), {x: 100});
   * ```
   *
   * @param {Element | object | string} scope
   * @returns {SelectorFunc} A selector function
   * @memberof gsap.utils
   */
  function selector(scope: Element | object | string | null): SelectorFunc;

  /**
   * Takes an array and randomly shuffles it, returning the same (but shuffled) array.
   * 
   * ```js
   * gsap.utils.shuffle(array);
   * ```
   *
   * @param {T[]} array
   * @returns {T[]} The same shuffled array
   * @memberof gsap.utils
   */
  function shuffle<T>(array: T[]): T[];

  /**
   * Snaps a value to the nearest increment of the number provided.
   * Or snaps to a value in the given array.
   * Or snaps to a value within the given radius (if an object is provided).
   * Or returns a function that does the above (if the second value is not provided).
   * 
   * ```js
   * gsap.utils.snap(10, 23.5); // 20
   * gsap.utils.snap([100, 50, 500], 65); // 50
   * gsap.utils.snap({values:[0, 100, 300], radius:20}, 30.5); // 30.5
   * gsap.utils.snap({increment:500, radius:150}, 310); // 310
   *
   * const snap = gsap.utils.snap(5); // no value = reusable function
   * console.log( snap(0.5) ); // 0
   * ```
   *
   * @param {SnapNumberConfig} snapConfig
   * @param {number} [valueToSnap]
   * @returns {number | Function} The snapped number or snap function
   * @memberof gsap.utils
   */
  function snap(snapConfig: number | number[] | SnapNumberConfig, valueToSnap: number): number;
  function snap(snapConfig: number | number[] | SnapNumberConfig): (valueToSnap: number) => number;
  /**
   * Snaps a value if within the given radius of a points (objects with "x" and "y" properties).
   * Or returns a function that does the above (if the second value is not provided).
   * 
   * ```js
   * 
   * gsap.utils.snap({values:[0, 100, 300], radius:20}, 85); // 100
   * 
   * const snap = gsap.utils.snap({values:[{x:0, y:0}, {x:10, y:10}, {x:20, y:20}], radius:5}); // no value = reusable function
   * console.log( snap({x:8, y:8}) ); // {x:10, y:10}
   * ```
   *
   * @param {SnapPoint2DConfig} snapConfig
   * @param {number} [valueToSnap]
   * @returns {Point2D | Function} The snapped number or snap function
   * @memberof gsap.utils
   */
  function snap(snapConfig: SnapPoint2DConfig, valueToSnap: Point2D): Point2D;
  function snap(snapConfig: SnapPoint2DConfig): (valueToSnap: Point2D) => Point2D;

  /**
   * Converts a string-based color value into an array consisting of RGB(A) or HSL values.
   * 
   * ```js
   * gsap.utils.splitColor("red"); // [255, 0, 0]
   * gsap.utils.splitColor("rgba(204, 153, 51, 0.5)"); // [204, 153, 51, 0.5]
   *
   * gsap.utils.splitColor("#6fb936", true); // [94, 55, 47] - HSL value
   * ```
   *
   * @param {string} color
   * @param {boolean} [hsl]
   * @returns {[number, number, number] | [number, number, number, number]} The converted color array
   * @memberof gsap.utils
   */
  function splitColor(color: string, hsl?: boolean): [number, number, number] | [number, number, number, number];

  /**
   * Converts almost anything into a flat Array.
   * 
   * ```js
   * const targets = gsap.utils.toArray(".class");
   * const targets = gsap.utils.toArray(myElement);
   * const targets = gsap.utils.toArray($(".class"));
   * const targets = gsap.utils.toArray([".class1", ".class2"]);
   * ```
   *
   * @param {string | object | Element | null} value
   * @param {object} [scope]
   * @param {boolean} [leaveStrings]
   * @returns {T[]} The converted Array
   * @memberof gsap.utils
   */
  function toArray<T>(value: string | object | Element | null, scope?: object | null, leaveStrings?: boolean): T[];

  /**
   * Ensures that a specific unit gets applied.
   * 
   * ```js
   * const clamp = gsap.utils.unitize( gsap.utils.clamp(0, 100), "px");
   * clamp(132); // "100px"
   * 
   * gsap.to(".class", {
   *   x: 1000,
   *   modifiers: {
   *     x: gsap.utils.unitize( gsap.utils.wrap(0, window.innerWidth), "px") 
   *   }
   * });
   * ```
   *
   * @param {Function} fn
   * @param {string} [unit]
   * @returns {string} The value with unit added
   * @memberof gsap.utils
   */
  function unitize<T extends Array<unknown>>(fn: (...args: T) => unknown, unit?: string): (...args: T) => string;

  /**
   * Returns the next number in a range after the given index, jumping to the start after the end has been reached.
   * 
   * ```js
   * let color = gsap.utils.wrap(["red", "green", "yellow"], 5); // "yellow"
   * 
   * let wrapper = gsap.utils.wrap(["red", "green", "yellow"]); // no value = reusable function
   * let color = wrapper(5) // "yellow"
   * ```
   *
   * @param {number} value1
   * @param {number} value2
   * @param {number} [index]
   * @returns {string} The wrapped value or wrap function
   * @memberof gsap.utils
   */
  function wrap(value1: number, value2: number, index: number): number;
  function wrap(value1: number, value2: number): (index: number) => number;
  /**
   * Returns the next item in an array after the given index, jumping to the start after the end has been reached.
   * 
   * ```js
   * let color = gsap.utils.wrap(["red", "green", "yellow"], 5); // "yellow"
   * 
   * let wrapper = gsap.utils.wrap(["red", "green", "yellow"]); // no value = reusable function
   * let color = wrapper(5) // "yellow"
   * ```
   *
   * @param {T[]} values
   * @param {number} [index]
   * @returns {string} The wrapper value or wrap function
   * @memberof gsap.utils
   */
  function wrap<T>(values: T[], index: number): T;
  function wrap<T>(values: T[]): (index: number) => T;

  /**
   * Returns the next number in a range after the given index, wrapping backwards towards the start after the end has been reached.
   * 
   * ```js
   * let color = gsap.utils.wrap(["red", "green", "yellow"], 5); // "yellow"
   * 
   * let wrapper = gsap.utils.wrap(["red", "green", "yellow"]); // no value = reusable function
   * let color = wrapper(5) // "yellow"
   * ```
   *
   * @param {number} value1
   * @param {number} value2
   * @param {number} [index]
   * @returns {string} The wrapped value or wrap function
   * @memberof gsap.utils
   */
  function wrapYoyo(value1: number, value2: number, index: number): number;
  function wrapYoyo(value1: number, value2: number): (index: number) => number;
  /**
   * Returns the next item in an array after the given index, wrapping backwards towards the start after the end has been reached.
   * 
   * ```js
   * let color = gsap.utils.wrap(["red", "green", "yellow"], 5); // "yellow"
   * 
   * let wrapper = gsap.utils.wrap(["red", "green", "yellow"]); // no value = reusable function
   * let color = wrapper(5) // "yellow"
   * ```
   *
   * @param {T[]} values
   * @param {number} [index]
   * @returns {string} The wrapper value or wrap function
   * @memberof gsap.utils
   */
  function wrapYoyo<T>(values: T[], index: number): T;
  function wrapYoyo<T>(values: T[]): (index: number) => T;
}

declare module "gsap/gsap-core" {
  export const clamp: typeof gsap.utils.clamp;
  export const distribute: typeof gsap.utils.distribute;
  export const getUnit: typeof gsap.utils.getUnit;
  export const interpolate: typeof gsap.utils.interpolate; 
  export const mapRange: typeof gsap.utils.mapRange;
  export const normalize: typeof gsap.utils.normalize;
  export const pipe: typeof gsap.utils.pipe;
  export const random: typeof gsap.utils.random;
  export const selector: typeof gsap.utils.selector;
  export const shuffle: typeof gsap.utils.shuffle;
  export const snap: typeof gsap.utils.snap;
  export const splitColor: typeof gsap.utils.splitColor;
  export const toArray: typeof gsap.utils.toArray;
  export const unitize: typeof gsap.utils.unitize;
  export const wrap: typeof gsap.utils.wrap;
  export const wrapYoyo: typeof gsap.utils.wrapYoyo;
}
