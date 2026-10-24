/** One baseline per published ready barrier. Missing revisions are legacy
 * invalidations, not proof that an existing baseline is still current. */
export function createRemoteHydrationOwner(run: () => Promise<boolean>) {
  let revision = -1;
  let request: Promise<void> | undefined;
  let installed = false;
  return {
    observe(next: number | undefined) {
      if (next === undefined) return true;
      if (next < revision) return false;
      if (next > revision) { revision = next; request = undefined; installed = false; }
      return true;
    },
    load(next?: number): Promise<void> {
      if (!this.observe(next)) return Promise.resolve();
      if (next !== undefined && (request || installed)) return request ?? Promise.resolve();
      installed = false;
      const pending = run().then(ready => { if (request === pending) installed = ready; })
        .finally(() => { if (request === pending) request = undefined; });
      request = pending;
      return pending;
    },
  };
}
