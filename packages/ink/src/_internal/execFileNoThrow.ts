import { execFile } from 'node:child_process';

type ExecResult = {
  code: number;
  stdout: string;
  stderr: string;
};

type ExecOpts = {
  cwd?: string;
  env?: NodeJS.ProcessEnv;
  timeout?: number;
  signal?: AbortSignal;
  input?: string;
};

// Variant of `execFile` that never rejects. Callers (mainly OSC integrations
// probing optional terminal helpers) treat a non-zero exit code as data, not
// an exceptional condition — turning each invocation into a try/catch would
// be noise. The 10 MiB buffer matches Node's default ceiling shape; anything
// larger from a terminal helper would already indicate something is wrong.
export function execFileNoThrow(
  file: string,
  args: readonly string[],
  opts: ExecOpts = {},
): Promise<ExecResult> {
  return new Promise((resolve) => {
    const child = execFile(
      file,
      args as string[],
      {
        cwd: opts.cwd,
        env: opts.env ?? process.env,
        timeout: opts.timeout,
        signal: opts.signal,
        encoding: 'utf8',
        maxBuffer: 10 * 1024 * 1024,
      },
      (err, stdout, stderr) => {
        // Node attaches a numeric exit code on ExecException for normal
        // non-zero exits, and a string code (e.g. 'ENOENT') for spawn
        // failures. We only forward numeric codes; anything else collapses
        // to a generic failure (1) so callers see a uniform shape.
        const errnoCode = (err as NodeJS.ErrnoException | null)?.code;
        const code =
          typeof errnoCode === 'number' ? errnoCode : err ? 1 : 0;
        resolve({
          code,
          stdout: stdout == null ? '' : String(stdout),
          stderr: stderr == null ? '' : String(stderr),
        });
      },
    );
    if (opts.input !== undefined && child.stdin) {
      child.stdin.end(opts.input);
    }
  });
}
