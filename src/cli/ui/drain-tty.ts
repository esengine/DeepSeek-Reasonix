/** stdin-queue drain on exit — eats stuck terminal-feature-detection responses (#365). */

import process from "node:process";

/** Eats stuck OSC/CPR/DA replies on exit so fish/bash don't print them as input (#365). */
export async function drainTtyResponses(timeoutMs = 50): Promise<void> {
  const stdin = process.stdin;
  if (!stdin.isTTY && typeof (stdin as { setRawMode?: unknown }).setRawMode !== "function") {
    return;
  }
  let raised = false;
  try {
    stdin.setRawMode(true);
    raised = true;
  } catch {
    return;
  }
  stdin.resume();

  await new Promise<void>((resolve) => {
    const onData = (_chunk: Buffer | string): void => {
      // Discard — anything pending here is a terminal-feature reply.
    };
    stdin.on("data", onData);
    const timer = setTimeout(() => {
      stdin.off("data", onData);
      stdin.pause();
      if (raised) {
        try {
          stdin.setRawMode(false);
        } catch {
          /* stdin may already be closed; ignore */
        }
      }
      resolve();
    }, timeoutMs);
    timer.unref();
  });
}
