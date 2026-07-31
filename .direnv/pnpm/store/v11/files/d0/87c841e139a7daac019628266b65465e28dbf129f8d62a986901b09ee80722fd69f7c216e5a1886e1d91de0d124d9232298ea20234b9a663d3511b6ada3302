import { spawn } from "node:child_process";
import { normalize } from "node:path";
//#region src/server/exec.ts
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
async function exec(_command, args = [], options = {}) {
	const command = normalize(_command);
	const signal = parseAbortSignal(options);
	const resolver = Promise.withResolvers();
	const process = spawn(command, args, {
		windowsHide: true,
		...options.spawnOptions,
		signal
	});
	handleStdin(process, options);
	process.once("close", () => {
		resolver.resolve();
	});
	process.once("error", (error) => {
		resolver.reject(error);
	});
	const stdoutPromise = process.stdout != null ? streamToString(process.stdout) : Promise.resolve("");
	const stderrPromise = process.stderr != null ? streamToString(process.stderr) : Promise.resolve("");
	await resolver.promise;
	const stdout = await stdoutPromise;
	const stderr = await stderrPromise;
	if ((options.throwOnNonZeroExitCode ?? true) && process.exitCode !== 0) throw new ExecError({
		pid: process.pid,
		stdout,
		stderr,
		exitCode: process.exitCode
	});
	return {
		pid: process.pid,
		stdout,
		stderr,
		exitCode: process.exitCode
	};
}
/**
* Represents an error thrown when a process exits with a non-zero exit code.
*
* @param result - The captured process result.
*/
var ExecError = class extends Error {
	result;
	constructor(result) {
		super(`Process exited with non-zero exit code (${result.exitCode})`);
		this.result = result;
		this.name = "ExecError";
	}
};
async function streamToString(stream) {
	return Buffer.concat(await stream.toArray()).toString("utf-8");
}
function handleStdin(process, options) {
	if (options.stdin == null) return;
	if (typeof options.stdin === "string") process.stdin?.end(options.stdin);
}
function parseAbortSignal(options) {
	const signals = [];
	if (options.signal != null) signals.push(options.signal);
	if (options.timeout != null) signals.push(AbortSignal.timeout(options.timeout));
	if (options.spawnOptions?.signal != null) signals.push(options.spawnOptions.signal);
	if (signals.length === 0) return;
	return AbortSignal.any(signals);
}
//#endregion
export { ExecError, exec };
