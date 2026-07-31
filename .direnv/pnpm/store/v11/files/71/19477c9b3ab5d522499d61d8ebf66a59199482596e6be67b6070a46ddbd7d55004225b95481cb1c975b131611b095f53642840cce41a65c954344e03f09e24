import { SpawnOptions } from "node:child_process";

//#region src/server/exec.d.ts
interface ExecOptions {
  signal?: AbortSignal;
  timeout?: number;
  stdin?: string;
  spawnOptions?: SpawnOptions;
  throwOnNonZeroExitCode?: boolean;
}
interface ExecResult {
  pid: number | undefined;
  stderr: string;
  stdout: string;
  exitCode: number | null;
}
/**
 * Executes a command and captures its output.
 *
 * This function spawns a child process, collects its stdout and stderr, and resolves with the
 * process result when execution finishes. By default, it throws an {@link ExecError} when the
 * process exits with a non-zero exit code.
 *
 * @param _command - The command to execute.
 * @param args - The arguments passed to the command.
 * @param options - The options object.
 * @param options.signal - An optional AbortSignal used to abort the process.
 * @param options.timeout - An optional timeout in milliseconds. When provided, the
 * process is aborted after the timeout expires.
 * @param options.stdin - An optional string written to the process stdin.
 * @param options.spawnOptions - Additional options forwarded to `child_process.spawn`.
 * @param options.throwOnNonZeroExitCode - Whether to throw an {@link ExecError} when the
 * process exits with a non-zero exit code. Defaults to `true`.
 * @returns A promise that resolves with the process result.
 * @throws {ExecError} Throws when `throwOnNonZeroExitCode` is enabled and the process exits with a
 * non-zero exit code.
 *
 * @example
 * const result = await exec('echo', ['hello']);
 *
 * console.log(result.stdout.trim());
 * // => 'hello'
 *
 * @example
 * const result = await exec('git', ['diff', '--quiet'], {
 *   throwOnNonZeroExitCode: false,
 * });
 *
 * console.log(result.exitCode);
 * // => 1 when there are changes
 */
declare function exec(_command: string, args?: string[], options?: ExecOptions): Promise<ExecResult>;
/**
 * Represents an error thrown when a process exits with a non-zero exit code.
 *
 * @param result - The captured process result.
 */
declare class ExecError extends Error {
  readonly result: ExecResult;
  constructor(result: ExecResult);
}
//#endregion
export { ExecError, exec };