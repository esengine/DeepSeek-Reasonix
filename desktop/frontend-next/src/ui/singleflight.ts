export function singleFlight(run: () => Promise<void>): () => Promise<void> {
  let flying: Promise<void> | null = null;
  let queued: Promise<void> | null = null;

  const start = () => {
    const p = run().finally(() => {
      if (flying === p) flying = null;
    });
    flying = p;
    return p;
  };
  const rerun = () => {
    queued = null;
    return start();
  };

  return () => {
    if (!flying) return start();
    queued ??= flying.then(rerun, rerun);
    return queued;
  };
}
