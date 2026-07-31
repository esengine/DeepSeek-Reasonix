//#region src/function/flow.d.ts
/**
 * Creates a new function that executes the given functions in sequence. The return value of the previous function is passed as an argument to the next function.
 *
 * The `this` context of the returned function is also passed to the functions provided as parameters.
 *
 * @param f The function to invoke.
 * @returns Returns the new composite function.
 *
 * @example
 * function noArgFunc() {
 *  return 42;
 * }
 *
 * const combined = flow(noArgFunc);
 * console.log(combined()); // 42
 */
declare function flow<R>(f: () => R): () => R;
/**
 * Creates a new function that executes the given functions in sequence. The return value of the previous function is passed as an argument to the next function.
 *
 * The `this` context of the returned function is also passed to the functions provided as parameters.
 *
 * @param f1 The function to invoke.
 * @returns Returns the new composite function.
 *
 * @example
 * function oneArgFunc(a: number) {
 *   return a * 2;
 * }
 *
 * const combined = flow(oneArgFunc);
 * console.log(combined(5)); // 10
 */
declare function flow<A extends any[], R>(f1: (...args: A) => R): (...args: A) => R;
/**
 * Creates a new function that executes the given functions in sequence. The return value of the previous function is passed as an argument to the next function.
 *
 * The `this` context of the returned function is also passed to the functions provided as parameters.
 *
 * @param f1 The function to invoke.
 * @param f2 The function to invoke.
 * @returns Returns the new composite function.
 *
 * @example
 * const add = (x: number, y: number) => x + y;
 * const square = (n: number) => n * n;
 *
 * const combined = flow(add, square);
 * console.log(combined(1, 2)); // 9
 */
declare function flow<A extends any[], R1, R2>(f1: (...args: A) => R1, f2: (a: R1) => R2): (...args: A) => R2;
/**
 * Creates a new function that executes the given functions in sequence. The return value of the previous function is passed as an argument to the next function.
 *
 * The `this` context of the returned function is also passed to the functions provided as parameters.
 *
 * @param f1 The function to invoke.
 * @param f2 The function to invoke.
 * @param f3 The function to invoke.
 * @returns Returns the new composite function.
 *
 * @example
 * const add = (x: number, y: number) => x + y;
 * const square = (n: number) => n * n;
 * const double = (n: number) => n * 2;
 *
 * const combined = flow(add, square, double);
 * console.log(combined(1, 2)); // 18
 */
declare function flow<A extends any[], R1, R2, R3>(f1: (...args: A) => R1, f2: (a: R1) => R2, f3: (a: R2) => R3): (...args: A) => R3;
/**
 * Creates a new function that executes the given functions in sequence. The return value of the previous function is passed as an argument to the next function.
 *
 * The `this` context of the returned function is also passed to the functions provided as parameters.
 *
 * @param f1 The function to invoke.
 * @param f2 The function to invoke.
 * @param f3 The function to invoke.
 * @param f4 The function to invoke.
 * @returns Returns the new composite function.
 *
 * @example
 * const add = (x: number, y: number) => x + y;
 * const square = (n: number) => n * n;
 * const double = (n: number) => n * 2;
 * const toStr = (n: number) => n.toString();
 *
 * const combined = flow(add, square, double, toStr);
 * console.log(combined(1, 2)); // '18'
 */
declare function flow<A extends any[], R1, R2, R3, R4>(f1: (...args: A) => R1, f2: (a: R1) => R2, f3: (a: R2) => R3, f4: (a: R3) => R4): (...args: A) => R4;
/**
 * Creates a new function that executes the given functions in sequence. The return value of the previous function is passed as an argument to the next function.
 *
 * The `this` context of the returned function is also passed to the functions provided as parameters.
 *
 * @param f1 The function to invoke.
 * @param f2 The function to invoke.
 * @param f3 The function to invoke.
 * @param f4 The function to invoke.
 * @param f5 The function to invoke.
 * @returns Returns the new composite function.
 *
 * @example
 * const add = (x: number, y: number) => x + y;
 * const square = (n: number) => n * n;
 * const double = (n: number) => n * 2;
 * const toStr = (n: number) => n.toString();
 * const split = (s: string) => s.split('');
 *
 * const combined = flow(add, square, double, toStr, split);
 * console.log(combined(1, 2)); // ['1', '8']
 */
declare function flow<A extends any[], R1, R2, R3, R4, R5>(f1: (...args: A) => R1, f2: (a: R1) => R2, f3: (a: R2) => R3, f4: (a: R3) => R4, f5: (a: R4) => R5): (...args: A) => R5;
/**
 * Creates a new function that executes the given functions in sequence. The return value of the previous function is passed as an argument to the next function.
 *
 * The `this` context of the returned function is also passed to the functions provided as parameters.
 *
 * @param funcs The functions to invoke.
 * @returns Returns the new composite function.
 *
 * @example
 * const add = (x: number, y: number) => x + y;
 * const square = (n: number) => n * n;
 *
 * const combined = flow(add, square);
 * console.log(combined(1, 2)); // 9
 */
declare function flow(...funcs: Array<(...args: any[]) => any>): (...args: any[]) => any;
//#endregion
export { flow };