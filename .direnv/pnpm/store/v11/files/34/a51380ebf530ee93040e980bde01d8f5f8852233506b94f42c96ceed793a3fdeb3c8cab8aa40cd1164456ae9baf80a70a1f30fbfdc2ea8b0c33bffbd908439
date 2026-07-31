//#region src/util/invariant.d.ts
/**
 * Asserts that a given condition is true. If the condition is false, an error is thrown with the provided message.
 *
 * @param condition - The condition to evaluate.
 * @param message - The error message to throw if the condition is false.
 * @returns Returns void if the condition is true.
 * @throws {Error} Throws an error if the condition is false.
 *
 * @example
 * // This call will succeed without any errors
 * invariant(true, 'This should not throw');
 *
 * // This call will fail and throw an error with the message 'This should throw'
 * invariant(false, 'This should throw');
 */
declare function invariant(condition: unknown, message: string): asserts condition;
/**
 * Asserts that a given condition is true. If the condition is false, an error is thrown with the provided error.
 *
 * @param condition - The condition to evaluate.
 * @param error - The error to throw if the condition is false.
 * @returns Returns void if the condition is true.
 * @throws {Error} Throws an error if the condition is false.
 *
 * @example
 * // This call will succeed without any errors
 * invariant(true, new Error('This should not throw'));
 *
 * class CustomError extends Error {
 *   constructor(message: string) {
 *     super(message);
 *   }
 * }
 *
 * // This call will fail and throw an error with the message 'This should throw'
 * invariant(false, new CustomError('This should throw'));
 */
declare function invariant(condition: unknown, error: Error): asserts condition;
//#endregion
export { invariant };