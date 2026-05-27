import { logForDebugging } from './_internal/debug.js';

/** Emit a debug-channel warning when `value` is set but isn't an integer. */
export function ifNotInteger(value: number | undefined, name: string): void {
  if (value === undefined) return;
  if (Number.isInteger(value)) return;
  logForDebugging(`${name} should be an integer, got ${value}`, {
    level: 'warn',
  });
}
