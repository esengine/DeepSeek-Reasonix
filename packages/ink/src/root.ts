import type { ReactNode } from 'react';
import { logForDebugging } from './_internal/debug.js';
import { Stream } from 'stream';
import type { FrameEvent } from './frame.js';
import Ink, { type Options as InkOptions } from './ink.js';
import instances from './instances.js';

export type RenderOptions = {
  /** Output stream the app paints into. */
  stdout?: NodeJS.WriteStream;
  /** Input stream the app reads from. */
  stdin?: NodeJS.ReadStream;
  /** Error stream. */
  stderr?: NodeJS.WriteStream;
  /** Whether to intercept Ctrl+C and unmount the tree. */
  exitOnCtrlC?: boolean;

  patchConsole?: boolean;

  /** Fires after each frame with timing and flicker telemetry. */
  onFrame?: (event: FrameEvent) => void;
  /** Internal opt-in switch consumed by specific code paths. */
  incrementalRendering?: boolean;
};

export type Instance = {
  /** Replace the root with a new node, or update its props in place. */
  rerender: Ink['render'];
  /** Tear down the app and release its resources. */
  unmount: Ink['unmount'];
  /** Resolves when the app unmounts (via user, error, or unmount()). */
  waitUntilExit: Ink['waitUntilExit'];
  cleanup: () => void;
};

export type Root = {
  render: (node: ReactNode) => void;
  unmount: () => void;
  waitUntilExit: () => Promise<void>;
};

/** Mount a React tree and start painting it to the terminal. */
export const renderSync = (
  node: ReactNode,
  options?: NodeJS.WriteStream | RenderOptions,
): Instance => {
  const opts = getOptions(options);
  const inkOptions: InkOptions = {
    stdout: process.stdout,
    stdin: process.stdin,
    stderr: process.stderr,
    exitOnCtrlC: true,
    patchConsole: true,
    ...opts,
  };

  const instance: Ink = getInstance(
    inkOptions.stdout,
    () => new Ink(inkOptions),
  );

  instance.render(node);

  return {
    rerender: instance.render,
    unmount() {
      instance.unmount();
    },
    waitUntilExit: instance.waitUntilExit,
    cleanup: () => instances.delete(inkOptions.stdout),
  };
};

/** Async render entrypoint. */
const wrappedRender = async (
  node: ReactNode,
  options?: NodeJS.WriteStream | RenderOptions,
): Promise<Instance> => {
  await Promise.resolve();
  const instance = renderSync(node, options);
  logForDebugging(
    `[render] first ink render: ${Math.round(process.uptime() * 1000)}ms since process start`,
  );
  return instance;
};

export default wrappedRender;

/** Create a Root without mounting anything yet. */
export async function createRoot({
  stdout = process.stdout,
  stdin = process.stdin,
  stderr = process.stderr,
  exitOnCtrlC = true,
  patchConsole = true,
  onFrame,
}: RenderOptions = {}): Promise<Root> {
  // Same microtask-boundary reasoning as wrappedRender above.
  await Promise.resolve();
  const instance = new Ink({
    stdout,
    stdin,
    stderr,
    exitOnCtrlC,
    patchConsole,
    onFrame,
  });

  // Index the instance by its stdout so code that looks it up later (e.g.
  // the external editor pause/resume flow) can find it.
  instances.set(stdout, instance);

  return {
    render: node => instance.render(node),
    unmount: () => instance.unmount(),
    waitUntilExit: () => instance.waitUntilExit(),
  };
}

function getOptions(
  stdout: NodeJS.WriteStream | RenderOptions | undefined = {},
): RenderOptions {
  // Historical convenience: passing a stream directly is shorthand for
  // `{ stdout: stream }`. We sniff with `instanceof Stream` rather than
  // duck-typing so accidental plain-object args fall through to the else.
  if (stdout instanceof Stream) {
    return {
      stdout,
      stdin: process.stdin,
    };
  }

  return stdout;
}

function getInstance(
  stdout: NodeJS.WriteStream,
  createInstance: () => Ink,
): Ink {
  let instance = instances.get(stdout);

  if (!instance) {
    instance = createInstance();
    instances.set(stdout, instance);
  }

  return instance;
}
